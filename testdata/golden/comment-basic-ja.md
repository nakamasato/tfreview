<!-- tfreview:begin -->
## 🔴 危険度: critical — Destruction / downtime

![critical](https://img.shields.io/badge/risk-critical-B60205) ![Destruction / downtime](https://img.shields.io/badge/Destruction%20%2F%20downtime-1%2F2-B60205) ![Permissions / exposure](https://img.shields.io/badge/Permissions%20%2F%20exposure-0%2F1-0E8A16)

判定時刻 <relative-time datetime="2026-09-02T00:00:00Z">2026-09-02T00:00:00Z</relative-time> 対象 commit [`abc1234`](https://github.com/o/r/commit/abc1234def5678).

| 観点 | 危険度 | 該当 |
| --- | --- | --- |
| Destruction / downtime | 🔴 critical | 1/2 |
| Permissions / exposure | 🟢 none | 0/1 |

<details><summary>チェック</summary>

| チェック | レベル | 判定 | 根拠 |
| --- | --- | --- | --- |
| delete-or-replace | critical | 🤖 hit | aws_db_instance.main is deleted \| replaced |
| shared | critical | 🔧 miss | no resource matched |
| sg-open | high | 🤖 miss | no security group changed |

</details>

<details><summary>対象</summary>

| 対象 | + | ~ | - | ± | import | 判定 |
| --- | --- | --- | --- | --- | --- | --- |
| prd | 0 | 0 | 1 | 0 | 0 | 再判定 |
| dev | 1 | 0 | 0 | 0 | 0 | 再利用 |

</details>

観点ファイル: [`.tfreview.yaml`](https://github.com/o/r/blob/abc1234def5678/.tfreview.yaml)

<sub>claude-opus-5 · 2 回 · in 2,000 / cache write 0 / cache read 0 / out 200 tokens · ≈ $0.0150</sub>
<!-- tfreview:end -->
