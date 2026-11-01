# sing-box native protocol

`daemon/started_service.proto` is an unmodified copy of the official
[sing-box v1.14.2 protocol](https://github.com/SagerNet/sing-box/blob/v1.14.2/daemon/started_service.proto)
(Git blob `2c27937b6c58e4fae1eb9cd2b231e45841be7838`). Keep its `daemon`
namespace, field numbers and upstream Go option intact.

The copied schema is unchanged in stable v1.14.4 and the reviewed upstream
testing commit `e5d91fa79f6319d5de940d6c81d81f0fe53b9476`; the native API version
remains 4. Compatibility tests use the actual v1.14.4 and v1.15.0-alpha.13
binaries. The v1.14.2 link above records the original copy, not the latest core
release. See [integration and validation](../docs/upstream-integration.md).

This directory is a separate Buf workspace. Its minimal lint configuration
accepts upstream naming without weakening lint for the application's protocols.
`buf.native.gen.yaml` overrides the Go package only during generation and pins
the Go, Connect Go and protobuf-es generators. No sing-box Go dependency or
grpc-go runtime is needed.

From the repository root:

```sh
buf lint proto-native
buf generate proto-native --template buf.native.gen.yaml
```

Generated files live in `gen/native/daemon` and `frontend/gen/native/daemon`.
Both sides consume this same official service. The HTTP backend forwards its
gRPC-Web frames without introducing another application control protocol.
