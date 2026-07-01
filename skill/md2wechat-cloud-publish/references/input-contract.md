# Input contract

The upstream content system must provide a complete, immutable handoff artifact before publishing.

## Markdown

Use UTF-8 Markdown with optional frontmatter:

```yaml
---
title: "Required article title"
author: "Optional author"
digest: "Optional digest"
---
```

The final resolved title must not exceed 32 characters, author 16 characters, or digest 128 characters. `inspect` is the source of truth for current limits and blockers.

## Images

Use local or HTTPS references:

```markdown
![description](./assets/image.png)
![description](https://public.example.com/image.png)
```

Local paths are resolved relative to the Markdown file. Ensure files exist and are readable by the runtime user. HTTPS sources must be reachable from the Tencent Cloud instance.

Do not hand off unresolved generation placeholders such as `![description](__generate:create an image__)`. Generate the image upstream and replace the placeholder with a local path or HTTPS URL.

## Cover

Supply exactly one of:

- a readable local cover file through `--cover`;
- an existing WeChat permanent-material ID through `--cover-media-id`.

Keep the article, local images, and cover in the same immutable job directory until the command finishes.
