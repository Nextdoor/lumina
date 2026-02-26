# Lumina Documentation Website

This directory contains the [Hugo](https://gohugo.io/) + [Docsy](https://www.docsy.dev/) documentation site for Lumina, deployed to `oss.nextdoor.com/lumina`.

## Prerequisites

- [Hugo Extended](https://gohugo.io/installation/) v0.146.0+
- [Go](https://go.dev/dl/) 1.21+ (for Hugo modules)
- [Node.js](https://nodejs.org/) (for PostCSS/Autoprefixer)

## Quick Start

```bash
# Install Node dependencies (first time only)
npm install

# Start the local dev server
hugo server

# The site will be available at:
#   http://localhost:1313/lumina/
```

## Common Commands

```bash
# Dev server with draft content
hugo server --buildDrafts

# Dev server with full rebuilds on every change
hugo server --disableFastRender

# Production build
hugo --minify

# Update Docsy theme module
hugo mod get -u github.com/google/docsy
hugo mod tidy
```

## Directory Structure

```
website/
├── hugo.yaml              # Hugo configuration
├── package.json           # Node.js dependencies (PostCSS)
├── go.mod                 # Go module (Docsy theme)
├── content/en/            # All site content (Markdown)
│   ├── _index.md          # Landing page
│   └── docs/              # Documentation pages
├── layouts/               # Custom Hugo templates
├── assets/scss/           # Custom styles
└── public/                # Build output (gitignored)
```

## Adding Content

All documentation lives in `content/en/docs/`. Pages use Hugo front matter for ordering:

```yaml
---
title: "Page Title"
description: "Brief description"
weight: 10
---
```

Lower `weight` values appear first in the sidebar. Section landing pages use `_index.md`.

## Deployment

The site is automatically built and deployed via GitHub Actions on push to `main`. The production build uses `hugo --minify` and deploys to GitHub Pages at `oss.nextdoor.com/lumina`.
