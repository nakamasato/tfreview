# Changelog

## [0.2.3](https://github.com/nakamasato/tfreview/compare/v0.2.2...v0.2.3) (2026-10-05)


### Bug Fixes

* keep per-phase verdicts for reused targets ([#33](https://github.com/nakamasato/tfreview/issues/33)) ([d9de7c6](https://github.com/nakamasato/tfreview/commit/d9de7c619ac6d638b1941a75b07af70ddf1ae5f5))

## [0.2.2](https://github.com/nakamasato/tfreview/compare/v0.2.1...v0.2.2) (2026-10-04)


### Features

* approve the PR when all checks are clear ([#28](https://github.com/nakamasato/tfreview/issues/28)) ([1950767](https://github.com/nakamasato/tfreview/commit/1950767ddd65c55530ae4e01b4f1b1173f44947c))
* improve PR comment layout ([#30](https://github.com/nakamasato/tfreview/issues/30)) ([b925644](https://github.com/nakamasato/tfreview/commit/b9256445920456cd3206258e69df190630880486))

## [0.2.1](https://github.com/nakamasato/tfreview/compare/v0.2.0...v0.2.1) (2026-10-04)


### Features

* add HTML output to review command ([#27](https://github.com/nakamasato/tfreview/issues/27)) ([d9e0386](https://github.com/nakamasato/tfreview/commit/d9e0386c234cadc9cb922b6e2612c5c00a55a61b))
* notify when tfreview updates are available ([#23](https://github.com/nakamasato/tfreview/issues/23)) ([55c6fdb](https://github.com/nakamasato/tfreview/commit/55c6fdbea6317ab9417cbaac4cf092f28b9201e5))
* show per-phase check results and usage ([#24](https://github.com/nakamasato/tfreview/issues/24)) ([2a01f32](https://github.com/nakamasato/tfreview/commit/2a01f32f0c999260f2bbade5be44d20048d8fc94))

## [0.2.0](https://github.com/nakamasato/tfreview/compare/v0.1.0...v0.2.0) (2026-09-21)


### ⚠ BREAKING CHANGES

* score each change with Jev and settle undecided checks with a plan-scoped agent ([#22](https://github.com/nakamasato/tfreview/issues/22))
* accumulate review knowledge per resource type ([#17](https://github.com/nakamasato/tfreview/issues/17))

### Features

* accumulate review knowledge per resource type ([#17](https://github.com/nakamasato/tfreview/issues/17)) ([f9628fd](https://github.com/nakamasato/tfreview/commit/f9628fdd3a84abdea1f1c25a45ae6aa30ed5282c))
* add the tfreview-rules skill for generating checkpoints from PR history ([#20](https://github.com/nakamasato/tfreview/issues/20)) ([08f90ad](https://github.com/nakamasato/tfreview/commit/08f90ade7f5cd038edb1a35723ebc01334f593ef))
* judge through the local claude CLI and score verdicts against fixtures ([#19](https://github.com/nakamasato/tfreview/issues/19)) ([c87de47](https://github.com/nakamasato/tfreview/commit/c87de474be397650bf780b4989b4ad931c7bece3))
* score each change with Jev and settle undecided checks with a plan-scoped agent ([#22](https://github.com/nakamasato/tfreview/issues/22)) ([a080f49](https://github.com/nakamasato/tfreview/commit/a080f490243464d9bf73f98d3a994a5fde9afc4a))

## 0.1.0 (2026-09-04)


### Features

* after_unknown を考慮して computed 属性を区別する ([#8](https://github.com/nakamasato/tfreview/issues/8)) ([7494533](https://github.com/nakamasato/tfreview/commit/7494533602f9ba7ca4d70e3ec90efe0cec9764d6))
* initial implementation ([#1](https://github.com/nakamasato/tfreview/issues/1)) ([c4d01ba](https://github.com/nakamasato/tfreview/commit/c4d01ba2fa50b6bc0e0621da851e3ff7105d14bc))
* match.targets の typo を warning で検知する ([#6](https://github.com/nakamasato/tfreview/issues/6)) ([22524de](https://github.com/nakamasato/tfreview/commit/22524def2587bf98a7ac8f790fbf35deec163224))


### Bug Fixes

* --fail-on-rule-only ignored the incomplete gate ([#4](https://github.com/nakamasato/tfreview/issues/4)) ([33f9fa1](https://github.com/nakamasato/tfreview/commit/33f9fa189c24fdd5613f4b1b7722f90cdbbf6749))
* fetch/planfind の非決定性・パストラバーサルを修正 ([#10](https://github.com/nakamasato/tfreview/issues/10)) ([cc15834](https://github.com/nakamasato/tfreview/commit/cc158341a86fc35254d17bca202fdbcc37811b3f))
* ListArtifacts が runs/artifacts をページングしていない問題を修正 ([#9](https://github.com/nakamasato/tfreview/issues/9)) ([e65292f](https://github.com/nakamasato/tfreview/commit/e65292ffa7ee23272f4abdd5314fd03c42b4ccd7))
* LLM 応答が max_tokens で切れたことを検知する ([#7](https://github.com/nakamasato/tfreview/issues/7)) ([0ec9d7a](https://github.com/nakamasato/tfreview/commit/0ec9d7a0b9e1291a272daacf878a47098cf74d8f))
* PRコメントのマーカー破壊防止 + Incomplete見出しの空括弧を修正 ([#5](https://github.com/nakamasato/tfreview/issues/5)) ([1c3eeb6](https://github.com/nakamasato/tfreview/commit/1c3eeb6d263ed3bad62d2fb1247b046b712f406d))
