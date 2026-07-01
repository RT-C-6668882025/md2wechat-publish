---
name: md2wechat-cloud-publish
description: Validate, convert, upload, and create WeChat Official Account drafts from prepared Markdown on headless Linux or Tencent Cloud instances. Use when an upstream workflow has already generated the article and images and an Agent must safely inspect readiness, render WeChat HTML, upload assets, or create a draft without generating content.
---

# md2wechat Cloud Publish

Use `md2wechat-publish` as a non-interactive publishing boundary. Treat the caller's Markdown and images as final generation artifacts; do not choose text models, image models, prompts, or content strategy.

## Preconditions

1. Require `md2wechat-publish` on `PATH`.
2. Require a complete Markdown file and resolved local or HTTPS image references.
3. Read [references/input-contract.md](references/input-contract.md) when preparing or validating handoff artifacts.
4. Read [references/tencent-cloud.md](references/tencent-cloud.md) for Tencent Cloud networking, credentials, containers, and scheduling.

## Publishing workflow

Run the readiness check before every side effect:

```bash
md2wechat-publish inspect article.md --draft --cover cover.jpg
```

Parse the JSON envelope. Continue only when `success` is true and `data.readiness.targets.draft` is `ready`. Report blockers without modifying the generated article.

For conversion without WeChat side effects, always provide an output path:

```bash
md2wechat-publish convert article.md --output article.html
```

Create a draft only after explicit authorization:

```bash
md2wechat-publish draft article.md --cover cover.jpg
```

Use `--cover-media-id` instead of `--cover` when the caller supplies an existing permanent WeChat material ID. Never pass both.

## Automation contract

- Treat stdout as one JSON document with `success`, `schema_version`, and `data` or `error`.
- Treat a nonzero exit code or `success: false` as failure.
- Keep logs and status messages out of input Markdown.
- Retry `inspect` freely. Retry `convert` only when its output path can be safely replaced.
- Do not automatically retry `upload` or `draft` after an ambiguous network failure; verify external state first to avoid duplicate materials or drafts.
- Never print, persist, or return `WECHAT_SECRET` or API keys.
- Reject `__generate:...__` image placeholders. The upstream content system must resolve them before handoff.

## Responsibility boundary

The upstream system owns text generation, image generation, model routing, prompts, review, and final artifact selection. This skill owns readiness checks and publishing side effects only. A custom conversion service may be selected with `MD2WECHAT_BASE_URL`; it is not a content-generation provider.
