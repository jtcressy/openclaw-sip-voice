# Bridge API-Fit Spike

Disposable Go compile/API-fit spike for checking whether `sipgo` plus `diago`
can support the lightweight SIP UA/media bridge direction.

This module is intentionally isolated under `bridge/api-fit` and imports only
Go standard library packages plus:

- `github.com/emiago/diago`
- `github.com/emiago/diago/media`
- `github.com/emiago/diago/media/sdp`
- `github.com/emiago/sipgo`
- `github.com/emiago/sipgo/sip`
- `github.com/pion/rtp`

Run from this directory:

```sh
go mod tidy
go test ./...
```
