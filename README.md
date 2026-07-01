# md2wechat-publish

微信公众号发布 CLI。它只消费已经完成的 Markdown 和图片，负责检查、API 排版、素材上传、配置管理和草稿创建。

```bash
go run . inspect article.md --draft --cover cover.jpg
go run . convert article.md -o article.html
go run . draft article.md --cover cover.jpg
go run . upload cover.jpg
go run . config show
go run . config validate
```

发布命令遇到尚未解析的图片生成占位符时会拒绝创建草稿。上游系统必须先将它替换为本地文件或 HTTPS URL。

当前 module 自带发布所需的最小核心代码，不依赖仓库根 module，可以单独复制、构建和部署。内容生产系统不是本项目的运行时依赖。

腾讯云部署使用 [`deploy/Dockerfile`](deploy/Dockerfile)。可单独上传给云端 Agent 的发布 Skill 位于 [`skill/md2wechat-cloud-publish`](skill/md2wechat-cloud-publish)。内容与图片生成由上游系统完成，发布项目只接受最终 Markdown、图片和封面。

不使用容器时，可在本目录运行 `go build .`。仓库根目录的 `make build-publish-linux` 默认生成 Linux amd64 二进制；通过 `GOARCH=arm64 make build-publish-linux` 生成 ARM64 版本。
