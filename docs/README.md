# PoolMe Documentation

This directory contains the documentation website for PoolMe, built with VitePress and styled with Tailwind CSS.

## 🚀 Quick Start

### Prerequisites

- Node.js 18 or higher
- npm or yarn

### Installation

```bash
cd docs
npm install
```

### Development

Run the development server with hot reload:

```bash
npm run docs:dev
```

Visit `http://localhost:5173` to view the documentation.

### Build

Build the static site for production:

```bash
npm run docs:build
```

The built files will be in `docs/.vitepress/dist`.

### Preview

Preview the production build locally:

```bash
npm run docs:preview
```

## 📁 Project Structure

```
docs/
├── .vitepress/
│   ├── config.js           # VitePress configuration
│   └── theme/
│       ├── index.js        # Theme entry point
│       ├── custom.css      # Custom CSS styles
│       └── tailwind.css    # Tailwind CSS
├── guide/                  # User guides
│   ├── getting-started.md
│   ├── core-concepts.md
│   └── ...
├── api/                    # API documentation
│   ├── worker-pool.md
│   ├── options.md
│   └── ...
├── examples/               # Code examples
│   ├── basic.md
│   ├── streaming.md
│   └── ...
├── advanced/               # Advanced topics
│   └── ...
├── index.md               # Homepage
├── package.json
├── tailwind.config.js     # Tailwind configuration
└── postcss.config.js      # PostCSS configuration
```

## 🎨 Customization

### Tailwind CSS

Edit `tailwind.config.js` to customize:
- Colors (brand colors in `theme.extend.colors`)
- Fonts
- Animations
- Custom utilities

Custom Tailwind classes are defined in `.vitepress/theme/tailwind.css`:
- `.feature-card` - Card component
- `.code-example` - Code example wrapper
- `.badge` - Badge component
- `.hero-gradient` - Gradient text

### VitePress Theme

Edit `.vitepress/theme/custom.css` to customize:
- Brand colors (`--vp-c-brand-*`)
- Typography
- Component styles
- Dark mode colors

### Configuration

Edit `.vitepress/config.js` to customize:
- Site metadata (title, description)
- Navigation menu
- Sidebar structure
- Search settings
- Social links

## 🚢 Deployment

### GitHub Pages (Automatic)

The documentation is automatically deployed to GitHub Pages when you push to the `main` branch:

1. Ensure GitHub Pages is enabled in repository settings
2. Set source to "GitHub Actions"
3. Push changes to `main` branch
4. GitHub Actions will build and deploy automatically

### Manual Deployment

Build and deploy manually:

```bash
# Build
npm run docs:build

# The output is in .vitepress/dist
# Deploy this directory to any static hosting service
```

## 📝 Writing Documentation

### Markdown Features

VitePress supports:
- Standard Markdown
- GitHub Flavored Markdown
- Custom containers (tip, warning, danger)
- Code syntax highlighting
- Line highlighting in code blocks
- Import code snippets from files

Example:

```markdown
::: tip
This is a tip
:::

::: warning
This is a warning
:::

::: danger
This is dangerous
:::
```

### Code Blocks

```markdown
\`\`\`go {2,5-7}
package main

func main() {
    // Line 2 is highlighted
    // Lines 5-7 are highlighted
    fmt.Println("Hello")
}
\`\`\`
```

### Custom Components

Use Tailwind classes directly in markdown:

```markdown
<div class="feature-card">
  Custom styled card with Tailwind
</div>
```

## 🔧 Troubleshooting

### Port already in use

Change the port in `package.json`:

```json
"docs:dev": "vitepress dev docs --port 5174"
```

### Build fails

Clear cache and reinstall:

```bash
rm -rf node_modules package-lock.json
npm install
```

### Styles not updating

Restart the dev server or clear the VitePress cache:

```bash
rm -rf .vitepress/cache
npm run docs:dev
```

## 📚 Resources

- [VitePress Documentation](https://vitepress.dev/)
- [Tailwind CSS Documentation](https://tailwindcss.com/)
- [Markdown Guide](https://www.markdownguide.org/)

## 🤝 Contributing

To contribute to the documentation:

1. Fork the repository
2. Create a feature branch
3. Make your changes in the `docs/` directory
4. Test locally with `npm run docs:dev`
5. Submit a pull request

## 📄 License

MIT License - See LICENSE file for details
