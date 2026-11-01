package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/auth"
)

// ErrAborted は中止（Ctrl+C・Esc。FR-SET-7）。何も書かない。
var ErrAborted = errors.New("中止しました")

// TokenRequest は最後の「トークンを発行するか」の答え（FR-SET-10）。
type TokenRequest struct {
	Name  string
	Scope auth.Scope
}

// Answers は画面の答え全体。Token が nil なら発行しない。
type Answers struct {
	Values Values
	Token  *TokenRequest
}

// Prompter は画面。実体は huh の画面（HuhPrompter）。
type Prompter interface {
	// Ask は項目 1〜6 を順に聞く。中止は ErrAborted。
	Ask(ctx context.Context, st *State) (Answers, error)
	// ConfirmCommentLoss は、コメント入りの複数行配列を 1 行にするとキーの中のコメントが消えることの確認。
	// 了承されなければ false（そのキーは元のままにする）。
	ConfirmCommentLoss(ctx context.Context, key string) (bool, error)
}

// HuhPrompter は huh で画面を出す。In・Out は端末。
type HuhPrompter struct {
	In         io.Reader
	Out        io.Writer
	Home       string
	Candidates []string // listen_addr の候補（`IP:ポート`）
}

const manualEntry = "\x00manual"

// keyMap は既定に Esc の中止を足し、検索（フィルター）を無効にする。外部エディタは各 Text で ExternalEditor(false)（外部コマンドを実行しない。NFR-OPS-9）。
func keyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc"))
	// SetEnabled(false) では足りない。選択肢の読み込み（OptionsFunc）のたびに huh が Filter を有効へ戻し、ヘルプに / filter が出る。
	// キーの無い割り当てなら Enabled() は常に false。
	km.Select.Filter = key.NewBinding()
	return km
}

func (p *HuhPrompter) run(ctx context.Context, groups ...*huh.Group) error {
	err := huh.NewForm(groups...).WithInput(p.In).WithOutput(p.Out).WithKeyMap(keyMap()).RunWithContext(ctx)
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrAborted
	}
	return err
}

// ConfirmCommentLoss は 1 キーについて確認する。
func (p *HuhPrompter) ConfirmCommentLoss(ctx context.Context, k string) (bool, error) {
	ok := false
	err := p.run(ctx, huh.NewGroup(huh.NewConfirm().
		Title(k+" の配列の中のコメントが消えます").
		Description("複数行の配列を 1 行に書き換えるため、配列の中のコメントは残せません。\n「いいえ」なら "+k+" は元のままにします。").
		Affirmative("消してよい").Negative("元のままにする").Value(&ok)))
	return ok, err
}

// form は 1 回の Ask の入力欄の値。
type form struct {
	listenChoice, listenManual string
	sessions, defSession       string
	allowed, denied            string
	changeAgents               bool
	agents                     map[string]*Agent
	issue                      bool
	tokenName                  string
	tokenScope                 string
}

// Ask は FR-SET §11.1 の項目 1〜6 を順に聞く。
func (p *HuhPrompter) Ask(ctx context.Context, st *State) (Answers, error) {
	v := st.Cur.Values
	f := &form{
		sessions: strings.Join(v.Sessions, ", "),
		allowed:  strings.Join(v.AllowedRoots, "\n"),
		denied:   strings.Join(v.DeniedPaths, "\n"),
		agents:   map[string]*Agent{},
		// 2 つ以上のときだけ選ぶ。現在値が sessions に無ければ先頭（先頭は未設定と同じ扱い。P-27）
		defSession: v.DefaultSession,
		tokenScope: string(auth.ScopeOps),
	}
	var groups []*huh.Group
	if st.Exists && st.Invalid != nil {
		groups = append(groups, huh.NewGroup(huh.NewNote().
			Title("現在の config.toml の値が不正です").
			Description(st.Invalid.Error()+"\n\n画面の項目で直せるものは、次の画面から直してください。").
			Next(true).NextLabel("次へ")))
	}
	groups = append(groups, p.listenGroups(st, f)...)
	groups = append(groups, p.sessionGroups(f)...)
	groups = append(groups, p.pathGroups(f)...)
	groups = append(groups, p.agentGroups(st, f)...)
	groups = append(groups, p.tokenGroups(f)...)
	if err := p.run(ctx, groups...); err != nil {
		return Answers{}, err
	}
	return f.answers(), nil
}

func (f *form) answers() Answers {
	sessions := ParseSessions(f.sessions)
	a := Answers{Values: Values{
		Sessions:     sessions,
		AllowedRoots: ParsePaths(f.allowed),
		DeniedPaths:  ParsePaths(f.denied),
		Agents:       map[string]Agent{},
	}}
	a.Values.ListenAddr = f.listenChoice
	if f.listenChoice == manualEntry || f.listenChoice == "" {
		a.Values.ListenAddr = strings.TrimSpace(f.listenManual)
	}
	if len(sessions) >= 2 {
		a.Values.DefaultSession = sessions[0]
		for _, s := range sessions {
			if s == f.defSession {
				a.Values.DefaultSession = s
			}
		}
	}
	if f.changeAgents {
		for name, ag := range f.agents {
			a.Values.Agents[name] = Agent{
				Command: strings.TrimSpace(ag.Command), ExitCommand: strings.TrimSpace(ag.ExitCommand), AuthFile: strings.TrimSpace(ag.AuthFile),
			}
		}
	}
	if f.issue {
		a.Token = &TokenRequest{Name: f.tokenName, Scope: auth.Scope(f.tokenScope)}
	}
	return a
}

// listenGroups は項目 1。tailnet の候補があれば選択、末尾に「手で入力」。無ければ手入力だけ。
func (p *HuhPrompter) listenGroups(st *State, f *form) []*huh.Group {
	cur := strings.TrimSpace(st.Cur.Values.ListenAddr)
	f.listenManual = cur
	validate := func(s string) error { return ValidateListen(s) }
	input := huh.NewInput().Title("listen_addr（IP:ポート）").
		Description("tailnet の IP かループバック。0.0.0.0・::・ホスト名・ポート無しは使えません。").
		Placeholder("127.0.0.1:48120").Value(&f.listenManual).Validate(validate)
	if len(p.Candidates) == 0 {
		input.Description("このホストに tailnet のアドレスが見つかりません。tailscale が上がっていないかもしれません。\n" +
			"手で入力してください（tailnet の IP:ポート、またはテスト用のループバック 127.0.0.1:48120）。")
		return []*huh.Group{huh.NewGroup(input)}
	}
	var opts []huh.Option[string]
	f.listenChoice = p.Candidates[0]
	curOK := ValidateListen(cur) == nil
	inCandidates := false
	for _, c := range p.Candidates {
		inCandidates = inCandidates || c == cur
	}
	if curOK && !inCandidates {
		opts = append(opts, huh.NewOption(cur+"（現在値）", cur))
	}
	for _, c := range p.Candidates {
		opts = append(opts, huh.NewOption(c, c))
	}
	opts = append(opts, huh.NewOption("手で入力（ループバックなど）", manualEntry))
	switch {
	case curOK:
		f.listenChoice = cur
	case cur != "":
		f.listenChoice = manualEntry // 不正な現在値は手入力の欄で直す
	}
	sel := huh.NewSelect[string]().Title("listen_addr").
		Description("このホストの tailnet のアドレス（ポート 48120）から選びます。").
		Options(opts...).Value(&f.listenChoice)
	return []*huh.Group{
		huh.NewGroup(sel),
		huh.NewGroup(input).WithHideFunc(func() bool { return f.listenChoice != manualEntry }),
	}
}

// sessionGroups は項目 2。sessions はカンマ区切りの 1 行、2 つ以上のときだけ default_session を選ぶ。
func (p *HuhPrompter) sessionGroups(f *form) []*huh.Group {
	in := huh.NewInput().Title("sessions（カンマ区切り）").
		Description("監視する tmux セッション。既定は 0。").Value(&f.sessions).
		Validate(ValidateSessions)
	sel := huh.NewSelect[string]().Title("default_session").
		Description("既定のセッション。").
		OptionsFunc(func() []huh.Option[string] { return huh.NewOptions(ParseSessions(f.sessions)...) }, &f.sessions).
		Value(&f.defSession)
	return []*huh.Group{
		huh.NewGroup(in),
		huh.NewGroup(sel).WithHideFunc(func() bool { return len(ParseSessions(f.sessions)) < 2 }),
	}
}

// pathGroups は項目 3・4。1 行 1 パス。
func (p *HuhPrompter) pathGroups(f *form) []*huh.Group {
	allowed := huh.NewText().Title("allowed_roots（1 行 1 パス）").
		Description("作業フォルダ（cwd）に使ってよい根。~/ から書けます。改行は Ctrl+J、Enter で次へ。").
		CharLimit(4096).ExternalEditor(false).Value(&f.allowed).
		Validate(func(s string) error { return ValidatePaths("allowed_roots", s, p.Home, true) })
	denied := huh.NewText().Title("denied_paths（1 行 1 パス）").
		Description("作業フォルダ・ファイルにしてはいけないパス。\n" +
			"書くと既定（~/dotfiles）と入れ替わります（足されません）。~/dotfiles を守るなら残してください。").
		CharLimit(4096).ExternalEditor(false).Value(&f.denied).
		Validate(func(s string) error { return ValidatePaths("denied_paths", s, p.Home, false) })
	return []*huh.Group{huh.NewGroup(allowed), huh.NewGroup(denied)}
}

// agentGroups は項目 5。まず「変えますか」（既定は いいえ）、はい のときだけ各エージェントの 3 項目。
func (p *HuhPrompter) agentGroups(st *State, f *form) []*huh.Group {
	groups := []*huh.Group{huh.NewGroup(huh.NewConfirm().
		Title("エージェント（omp・claude・codex）の設定を変えますか").
		Description("command・exit_command・auth_file。空欄は上書きしません（組み込みの既定を使います）。").
		Affirmative("はい").Negative("いいえ").Value(&f.changeAgents))}
	for _, name := range agent.Builtin() {
		cur := st.Cur.Values.Agents[name]
		ag := &Agent{Command: cur.Command, ExitCommand: cur.ExitCommand, AuthFile: cur.AuthFile}
		f.agents[name] = ag
		def := AgentDefaults(name)
		field := func(label, fieldName string, val *string, example string) *huh.Input {
			k := agentKey(name, fieldName)
			return huh.NewInput().Title(name + " の " + label).Placeholder(example).Value(val).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" && st.Cur.Keys[k].Present {
						return errors.New("空にはできません（書かれているキーを消すときは config.toml を手で直してください）")
					}
					return ValidateAgentField(name, fieldName, s, p.Home)
				})
		}
		fields := []huh.Field{
			field("command", "command", &ag.Command, "既定: "+def.Command).Description("起動コマンド（" + fmt.Sprint(name) + "）"),
			field("exit_command", "exit_command", &ag.ExitCommand, "既定: "+def.ExitCommand).Description("終了コマンド"),
		}
		authExample := "既定: " + def.AuthFile
		if def.AuthFile == "" {
			authExample = "（既定なし）"
		}
		fields = append(fields, field("auth_file", "auth_file", &ag.AuthFile, authExample).Description("認証ファイルのパス（期限の確認に使う）"))
		groups = append(groups, huh.NewGroup(fields...).WithHideFunc(func() bool { return !f.changeAgents }))
	}
	return groups
}

// tokenGroups は項目 6（任意）。発行は保存のあと（Run）。
func (p *HuhPrompter) tokenGroups(f *form) []*huh.Group {
	confirm := huh.NewConfirm().Title("トークンを発行しますか").
		Description("保存のあとに `token add` と同じ処理で発行します。平文は 1 度だけ表示されます。").
		Affirmative("発行する").Negative("発行しない").Value(&f.issue)
	name := huh.NewInput().Title("トークンの名前").Description("小文字・数字・_・- で 32 文字まで。").
		Value(&f.tokenName).Validate(auth.ValidateName)
	scope := huh.NewSelect[string]().Title("窓口の範囲").
		Description("ops は操作ができます。report は読み取りだけです。").
		Options(huh.NewOptions(string(auth.ScopeOps), string(auth.ScopeReport))...).Value(&f.tokenScope)
	return []*huh.Group{
		huh.NewGroup(confirm),
		huh.NewGroup(name, scope).WithHideFunc(func() bool { return !f.issue }),
	}
}
