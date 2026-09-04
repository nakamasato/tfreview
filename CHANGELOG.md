# Changelog

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
