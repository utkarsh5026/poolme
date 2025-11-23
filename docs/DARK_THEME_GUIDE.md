# Dark Theme & Cascadia Code Setup

Your documentation is configured with a **beautiful dark theme** and **Cascadia Code** font by default!

## 🎨 Dark Theme Features

### Default Configuration
- **Dark mode is forced** - Site always loads in dark theme
- **VS Code inspired colors** - Familiar color scheme (#1e1e1e background)
- **High contrast** - Optimized for readability
- **Smooth gradients** - Blue → Purple → Pink accents

### Color Palette

```css
Primary:    #60a5fa  (Light Blue)
Secondary:  #a78bfa  (Light Purple)
Accent:     #f472b6  (Light Pink)
Background: #1e1e1e  (Dark Gray)
Soft BG:    #252526  (Slightly Lighter)
Code BG:    #1a1a1a  (Almost Black)
```

## 🔤 Cascadia Code Font

### What You Get
- **Font ligatures enabled** - `=>`, `!=`, `>=` render beautifully
- **Programming-optimized** - Clear distinction between similar characters
- **Fallback chain** - Cascadia Code → JetBrains Mono → Menlo → Monaco

### Ligatures Examples
When using Cascadia Code, these transform visually:

```
->  becomes  →
=>  becomes  ⇒
!=  becomes  ≠
==  becomes  ═
>=  becomes  ≥
<=  becomes  ≤
```

## 🛠️ Customization

### Change Colors

Edit `docs/tailwind.config.js`:

```js
colors: {
  poolme: {
    primary: '#your-color',    // Main brand color
    secondary: '#your-color',  // Secondary accent
    accent: '#your-color',     // Tertiary accent
  }
}
```

Also update `docs/.vitepress/theme/custom.css`:

```css
:root {
  --vp-c-brand-1: #your-color;
  --poolme-primary: #your-color;
}
```

### Switch to Light Theme (Not Recommended)

Remove from `docs/.vitepress/config.js`:

```js
// Remove or change this line
appearance: 'dark',
```

### Change Code Font

Edit `docs/tailwind.config.js`:

```js
fontFamily: {
  mono: ['"Your Font"', '"Fallback"', 'monospace']
}
```

## 🎯 Using Dark Theme Classes

### Tailwind Utilities

```markdown
<div class="feature-card">
  Automatically dark-themed card
</div>

<div class="glass-effect">
  Glassmorphism effect
</div>

<div class="dark-card">
  Enhanced dark card
</div>
```

### Custom Classes

```markdown
<!-- Gradient text -->
<h1 class="hero-gradient">Your Title</h1>
<h2 class="gradient-primary">Subtitle</h2>

<!-- Glowing text -->
<p class="glow-primary">Highlighted text</p>

<!-- Code blocks -->
<div class="code-block-dark">
  <code>Your code</code>
</div>
```

### Buttons

```markdown
<button class="btn-primary">Primary Action</button>
<button class="btn-secondary">Secondary Action</button>
```

## 📝 Code Block Styling

### Syntax Highlighting

Code blocks automatically use **GitHub Dark** theme:

````markdown
```go
func main() {
    fmt.Println("Dark themed!")
}
```
````

### Line Highlighting

Highlight specific lines:

````markdown
```go {2,4-6}
func example() {
    // Line 2 is highlighted
    x := 5
    y := 10
    z := x + y
    // Lines 4-6 are highlighted
}
```
````

### With Line Numbers

Line numbers are enabled by default:

````markdown
```go
// Automatic line numbers
func process() {
    // ...
}
```
````

## 🎨 Color Reference

### Background Layers

```
Level 0: #1a1a1a (Code blocks, deepest)
Level 1: #1e1e1e (Main background)
Level 2: #252526 (Soft elevation)
Level 3: #2d2d30 (Muted elevation)
```

### Text Colors

```
Primary:   rgba(255, 255, 255, 0.87)  (Main text)
Secondary: rgba(255, 255, 255, 0.6)   (Less important)
Tertiary:  rgba(255, 255, 255, 0.38)  (Disabled/hint)
```

### Border Colors

```
Default: rgba(255, 255, 255, 0.1)
Light:   rgba(255, 255, 255, 0.05)
Strong:  rgba(255, 255, 255, 0.2)
```

## 🔥 Pro Tips

### 1. Test Font Installation

If Cascadia Code doesn't load:

**Windows:** Install from [Microsoft's GitHub](https://github.com/microsoft/cascadia-code/releases)

**Mac:**
```bash
brew tap homebrew/cask-fonts
brew install --cask font-cascadia-code
```

**Linux:**
```bash
# Download and install from GitHub releases
```

### 2. Verify Dark Theme

Open browser dev tools and check:

```js
document.documentElement.classList.contains('dark') // Should be true
```

### 3. Custom Scrollbars

Already styled! Dark scrollbars with lighter thumb on hover.

### 4. Selection Color

Text selection is styled with primary color:

```css
::selection {
  background: rgba(96, 165, 250, 0.3);
  color: #ffffff;
}
```

## 🎯 Components Overview

### Feature Cards

```html
<div class="feature-card">
  <div class="text-2xl mb-2">🚀</div>
  <h3 class="font-semibold">Feature Title</h3>
  <p class="text-gray-400">Description</p>
</div>
```

### Hero Gradient

```html
<h1 class="hero-gradient text-6xl font-bold">
  Your Amazing Title
</h1>
```

### Code Examples

```html
<div class="code-example">
  <pre><code>const x = 42;</code></pre>
</div>
```

### Badges

```html
<span class="badge">New</span>
<span class="badge">Beta</span>
```

## 📊 Browser Support

- ✅ Chrome/Edge (full support)
- ✅ Firefox (full support)
- ✅ Safari (full support)
- ⚠️ IE11 (not supported - who cares? 😄)

## 🔧 Troubleshooting

### Font Not Loading

1. Check browser console for font errors
2. Verify font is installed on system
3. Try fallback fonts work
4. Clear browser cache

### Colors Look Wrong

1. Verify you're in dark mode: Check `html` has `class="dark"`
2. Check browser doesn't force light mode
3. Clear VitePress cache: `rm -rf docs/.vitepress/cache`

### Ligatures Not Working

1. Ensure Cascadia Code is installed
2. Check font-feature-settings in dev tools
3. Some terminals/editors need explicit enabling

### Build Issues

```bash
# Clear everything and rebuild
rm -rf docs/node_modules docs/.vitepress/cache
cd docs
npm install
npm run docs:dev
```

## 🚀 Performance

All styles are:
- ✅ Optimized with PostCSS
- ✅ Purged in production (unused CSS removed)
- ✅ Minified automatically
- ✅ Cached by browsers

Expected load time: **< 1 second** for initial page

## 📚 Resources

- [Cascadia Code](https://github.com/microsoft/cascadia-code)
- [VitePress Theming](https://vitepress.dev/guide/custom-theme)
- [Tailwind Dark Mode](https://tailwindcss.com/docs/dark-mode)
- [CSS Color Scheme](https://developer.mozilla.org/en-US/docs/Web/CSS/color-scheme)

---

**Enjoy your beautiful dark-themed documentation!** 🌙✨
