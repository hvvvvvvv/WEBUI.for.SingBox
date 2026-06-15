<div align="center">
  <img src="build/appicon.png" alt="webui.for.singbox" width="200">
  <h1>webui.for.singbox</h1>
  <p>A web GUI program developed with Vue 3 and Go.</p>
</div>

## Preview

Take a look at the live version here: 👉 <a href="https://gui-for-cores.github.io/guide/gfs/" target="_blank">Live Demo</a>

<div align="center">
  <img src="docs/imgs/light.png">
</div>

## Document

[Community](https://gui-for-cores.github.io/guide/gfs/community)

## Build

1、Build Environment

- Node.js [link](https://nodejs.org/en)

- pnpm ：`npm i -g pnpm`

- Go [link](https://go.dev/)

2、Pull and Build

```bash
git clone https://github.com/GUI-for-Cores/webui.for.singbox.git

cd webui.for.singbox/frontend

pnpm install --frozen-lockfile && pnpm build

cd ..

go build -trimpath -ldflags="-s -w" -o webui.for.singbox.server .
```

Run the server:

```bash
./webui.for.singbox.server --addr 0.0.0.0:9090
```

## Proto Code Generation

```bash
buf generate
```

## Stargazers over time

[![Stargazers over time](https://starchart.cc/GUI-for-Cores/webui.for.singbox.svg)](https://starchart.cc/GUI-for-Cores/webui.for.singbox)
