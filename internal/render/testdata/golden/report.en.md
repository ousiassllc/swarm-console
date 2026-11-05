# swarm-console report

- Time: 2026-10-31 22:10:05 (+09:00)
- Server load: load1 98.2 / CPU 42% / Mem free 41.3 GB / Swap 5%
- Disk: / 73% (120 GB free), /mnt/nvme 78% (830 GB free)
- Updates: omp 18.14.2 (Up to date) / swarm-console v0.1.5 (Up to date)
- Login: omp Unknown / claude Expired / codex Expiring soon

## Panes

| Pos | Agent | State | Elapsed | From | Repo / Issue | Doing now | Next |
|-----|-------|-------|---------|------|--------------|-----------|------|
| T1 | omp | ⚪ Idle | — | — | — | — | — |
| T2 | claude | 🟡 Asking | 3m | — | — | (2) の Go での範囲をどうしますか？ | — |
| B3 | omp | 🔴 Stalled | — | — | — | Waiting | — |
| B4 | omp | 🟢 Working | 42m | agent-b 🤖 かな発 | ousiassllc/funid #982 | Running go test ./... | 完了したら #983 を入れる |
| B1 | — | ⬜ Free | — | — | — | — | — |
| B2 | codex | ❔ Unknown | — | — | — | — | — |
| work:B5 | omp | 🟢 Working | — | x\|y 🤖 エージェント発 | o/r #1, #2 | a \| b c | — |

## Needs attention

- 🟡 Asking: T2 — (2) の Go での範囲をどうしますか？
- 🔴 Stalled: B3 — no change 20m
- 📩 Awaiting report: T1 — final report available (read with `report_read`)
- 🔑 Login expired: claude
- ⏳ Login expiring: codex — in 10 h 4 min (2026-11-01 08:15)

## Usage

### Claude

| Account | 5-hour | 7-day |
| --- | --- | --- |
| acct-a | Unavailable | 48% (Resets 2026-11-01 07:00) |
| account-1 | 0% | 7% (Resets 2026-11-06 10:00) |
| acct-c | 100% (Exhausted, Resets 2026-11-01 00:00) | 61.5% (Resets 2026-11-03 00:00) |
| acct-d | Unavailable | Unavailable |

### Cursor

| Account | Window | Used |
| --- | --- | --- |
| acct-a | monthly | 0% (Resets 2026-12-01 09:00) |
| account-1 | weekly | 0 USD (Resets 2026-11-02 09:00) |

### Codex

| Account | 5-hour | 7-day |
| --- | --- | --- |
| acct-a | 0% (Resets 2026-11-02 03:09) | 2% (Resets 2026-11-04 09:00) |

## Warnings

- 並びが変わりました: B3 が追加されました
