# sing-box native protocol

`daemon/started_service.proto` is an unmodified copy of the official
[sing-box v1.14.2 protocol](https://github.com/SagerNet/sing-box/blob/v1.14.2/daemon/started_service.proto)
(Git blob `2c27937b6c58e4fae1eb9cd2b231e45841be7838`). Keep its `daemon`
namespace, field numbers and upstream Go option intact.

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
