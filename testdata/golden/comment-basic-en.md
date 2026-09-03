<!-- tfreview:begin -->
## 🔴 Risk: critical — Destruction / downtime

![critical](https://img.shields.io/badge/risk-critical-B60205) ![Destruction / downtime](https://img.shields.io/badge/Destruction%20%2F%20downtime-1%2F2-B60205) ![Permissions / exposure](https://img.shields.io/badge/Permissions%20%2F%20exposure-0%2F1-0E8A16)

Judged at <relative-time datetime="2026-09-02T00:00:00Z">2026-09-02T00:00:00Z</relative-time> for [`abc1234`](https://github.com/o/r/commit/abc1234def5678).

| Category | Risk | Hits |
| --- | --- | --- |
| Destruction / downtime | 🔴 critical | 1/2 |
| Permissions / exposure | 🟢 none | 0/1 |

<details><summary>Checks</summary>

| Check | Level | Verdict | Reason |
| --- | --- | --- | --- |
| delete-or-replace | critical | 🤖 hit | aws_db_instance.main is deleted \| replaced |
| shared | critical | 🔧 miss | no resource matched |
| sg-open | high | 🤖 miss | no security group changed |

</details>

<details><summary>Targets</summary>

| Target | + | ~ | - | ± | import | Judgement |
| --- | --- | --- | --- | --- | --- | --- |
| prd | 0 | 0 | 1 | 0 | 0 | re-judged |
| dev | 1 | 0 | 0 | 0 | 0 | reused |

</details>

Criteria: [`.tfreview.yaml`](https://github.com/o/r/blob/abc1234def5678/.tfreview.yaml)

<sub>claude-opus-5 · 2 calls · in 2,000 / cache write 0 / cache read 0 / out 200 tokens · ≈ $0.0150</sub>
<!-- tfreview:end -->
