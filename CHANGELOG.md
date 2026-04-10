# Changelog

## 0.1.0 (2026-04-10)


### Features

* **ci:** add CI/CD pipeline, deployment scripts, and smoke test binary ([9e51304](https://github.com/rutabageldev/navi/commit/9e5130470b9585f305c3dfdd81c5eb83a57ad766))
* **digest:** implement P0 hello world service skeleton ([1cfa47d](https://github.com/rutabageldev/navi/commit/1cfa47d26a6113965b5da23c0d74a3e43283cca5))
* **digest:** P0 hello world — full service, observability, and CI/CD pipeline ([28b8630](https://github.com/rutabageldev/navi/commit/28b86300c9c9e6a757b082ca3f45f36ee2ac8685))
* **infra:** fix staging/prod compose for bridge networking and shared NATS ([9920a46](https://github.com/rutabageldev/navi/commit/9920a46f8e336a70983239d40a48f28f0258f15b))
* **infra:** Phase 2 services/internal package stubs ([334a379](https://github.com/rutabageldev/navi/commit/334a3796246ebeaa8649a828171c3c9a306fe81e))
* **infra:** Phase 4 Vault seeding and token renewal ([a13fc0e](https://github.com/rutabageldev/navi/commit/a13fc0edbeb93062a922c871cd9c224a86e05cec))
* **monitoring:** add P0 Grafana dashboard for navi-digest ([ed62ce7](https://github.com/rutabageldev/navi/commit/ed62ce776fb1fe7f5f4a0f222e3e0ab56a3cfdbf))
* **nats:** implement NKEY + mTLS authentication (Phase 3b) ([7531901](https://github.com/rutabageldev/navi/commit/7531901de1b21d1008e87232f25079dca12f149e))


### Bug Fixes

* **ci:** apply second-pass DevOps review feedback ([fbe96eb](https://github.com/rutabageldev/navi/commit/fbe96eb28075ee19bfe41a01c2923cf47cc1edbd))
* **ci:** correct trivy-action tag to v0.35.0 ([893cde9](https://github.com/rutabageldev/navi/commit/893cde93a14e64f843db0ebe1728252e7298da51))
* **ci:** fix golangci-lint v2 config schema and resolve lint findings ([4195f5c](https://github.com/rutabageldev/navi/commit/4195f5c7084364988526cbcc049d420d9a1fc21e))
* **ci:** force initial release to 0.1.0 via release-as override ([5b519e3](https://github.com/rutabageldev/navi/commit/5b519e377efa69d7782d963b5845d7feca442886))
* **ci:** force initial release to 0.1.0 via release-as override ([38b3fc0](https://github.com/rutabageldev/navi/commit/38b3fc0208224ccace75b69dc2fa881cb3cb9db5))
* **ci:** install oapi-codegen before check-generated in build job ([7041670](https://github.com/rutabageldev/navi/commit/7041670ac9fc52ccfcf43acbeea5a7cc2317057f))
* **ci:** pin Go to 1.25.9 and fix govulncheck module path ([a42699b](https://github.com/rutabageldev/navi/commit/a42699be59fe50732a0bc17e72135c900a349462))
* **ci:** remediate DevOps review findings ([46e9b37](https://github.com/rutabageldev/navi/commit/46e9b372d2c83a949bc299e3abb98471ac5ba5cd))
* **ci:** source Twilio credentials from .env on runner instead of GitHub secrets ([a0b17b2](https://github.com/rutabageldev/navi/commit/a0b17b22a80127f277273e37fa4d23e89ab7d0cb))
* **ci:** source VAULT_TOKEN from .env on runner instead of GitHub secrets ([720a454](https://github.com/rutabageldev/navi/commit/720a454b4d4ae2c72fbece076af9d6e5de70d805))
* **ci:** switch to googleapis/release-please-action@v4 ([c5d331c](https://github.com/rutabageldev/navi/commit/c5d331cb1a74ec389489140ea67b6a5b5b41786f))
* **ci:** switch to googleapis/release-please-action@v4 ([e66ea43](https://github.com/rutabageldev/navi/commit/e66ea43a037b97b478dc173289d78de6db2a9d6c))
* **ci:** upgrade golangci-lint-action to v7 for golangci-lint v2 support ([c2a1db4](https://github.com/rutabageldev/navi/commit/c2a1db4e0e9c587b8e48971969d9a498d9897386))
* **ci:** use per-module paths for go vet, test, build, and govulncheck ([77ac316](https://github.com/rutabageldev/navi/commit/77ac3168f5ac4079f31e19f28310dae1d4c1b91d))
* **deps:** upgrade go-jose/v4 to v4.1.4 (CVE-2026-34986) ([3a6b58e](https://github.com/rutabageldev/navi/commit/3a6b58e3a746a053c5e93f6416e33f466223e3f4))
* **digest:** register health routes directly and add ADR-0010 baseline middleware ([a6f2b36](https://github.com/rutabageldev/navi/commit/a6f2b365f39e1c55be1d82ee181cdd13d28f8798))
* **infra:** change prod digest port from 8080 to 8083 ([bb10af2](https://github.com/rutabageldev/navi/commit/bb10af2e8fc74fee983cfab26a67e695bf32bfed))
