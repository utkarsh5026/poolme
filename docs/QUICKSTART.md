# Quick Start - Documentation Website

Get your PoolMe documentation website running in 5 minutes!

## 🌙 Dark Theme + Cascadia Code

Your documentation comes pre-configured with:
- **Beautiful dark theme** (VS Code inspired colors)
- **Cascadia Code font** with ligatures enabled
- **High contrast** for excellent readability

See [DARK_THEME_GUIDE.md](DARK_THEME_GUIDE.md) for customization options.

## 🚀 Setup (First Time)

```bash
# 1. Navigate to docs directory
cd docs

# 2. Install dependencies
npm install

# 3. Start development server
npm run docs:dev
```

Visit **http://localhost:5173** in your browser - dark theme loads automatically!

## 📝 Common Tasks

### Add a New Page

1. Create a markdown file:
   ```bash
   touch docs/guide/my-new-page.md
   ```

2. Add content:
   ```markdown
   # My New Page

   Your content here...
   ```

3. Add to sidebar in `docs/.vitepress/config.js`:
   ```js
   sidebar: [
     {
       text: 'Guide',
       items: [
         { text: 'My New Page', link: '/guide/my-new-page' }
       ]
     }
   ]
   ```

### Update Colors

Edit `docs/tailwind.config.js`:
```js
colors: {
  poolme: {
    primary: '#your-color',    // Change this
    secondary: '#your-color',
    accent: '#your-color',
  }
}
```

### Change Repository URL

Update in `docs/.vitepress/config.js`:
```js
base: '/your-repo-name/', // Match your GitHub repo name
```

## 🌐 Deploy to GitHub Pages

### One-Time Setup

1. Go to GitHub repository → Settings → Pages
2. Under "Build and deployment" → Source: **GitHub Actions**
3. That's it!

### Deploy

Just push to main branch:
```bash
git add .
git commit -m "Update docs"
git push origin main
```

Your site will be live at:
```
https://your-username.github.io/your-repo-name/
```

## 🎨 Customization Checklist

- [ ] Update `base` URL in `.vitepress/config.js`
- [ ] Change brand colors in `tailwind.config.js`
- [ ] Replace logo in `docs/public/logo.svg`
- [ ] Update site title and description in `config.js`
- [ ] Add your content pages
- [ ] Test locally with `npm run docs:dev`
- [ ] Push to GitHub

## 📚 Project Structure

```
docs/
├── .vitepress/
│   ├── config.js          # Site configuration
│   └── theme/
│       ├── custom.css     # Custom styles
│       └── tailwind.css   # Tailwind utilities
├── guide/                 # User guides
├── api/                   # API documentation
├── examples/              # Code examples
├── index.md              # Homepage
└── package.json
```

## 💡 Writing Tips

### Code Blocks
````markdown
```go
func main() {
    fmt.Println("Hello")
}
```
````

### Custom Containers
```markdown
::: tip
Helpful tip here
:::
```

### Tailwind Classes
```markdown
<div class="grid grid-cols-2 gap-4">
  <div class="feature-card">Card 1</div>
  <div class="feature-card">Card 2</div>
</div>
```

## 🔧 Build Commands

```bash
# Development server (hot reload)
npm run docs:dev

# Production build
npm run docs:build

# Preview production build
npm run docs:preview
```

## 🆘 Need Help?

- **Detailed Guide**: See [DOCUMENTATION_SETUP.md](../DOCUMENTATION_SETUP.md)
- **VitePress Docs**: https://vitepress.dev/
- **Tailwind Docs**: https://tailwindcss.com/

## ✅ Verification Checklist

After setup, verify:

- [ ] Development server runs (`npm run docs:dev`)
- [ ] All pages load without errors
- [ ] Navigation links work
- [ ] Code examples display correctly
- [ ] Tailwind styles are applied
- [ ] Dark mode toggle works
- [ ] Build succeeds (`npm run docs:build`)
- [ ] GitHub Actions workflow is enabled
- [ ] Site deploys to GitHub Pages

---

**That's it!** You're ready to create amazing documentation for your Go project. 🎉
