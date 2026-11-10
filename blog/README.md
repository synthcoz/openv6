# Blog posts

Each file in `posts/` is one article, served at `https://thephuc.xyz/openv6/blog/<file-name>/`.

A post starts with a JSON header inside an HTML comment, followed by the article body in plain HTML (`<p>`, `<h2 id="…">`, `<h3>`, `<ul>`, `<ol>`, `<table>`, `<pre><code>`). Every `<h2>` needs an `id`; it feeds the table of contents.

```html
<!--
{
  "title": "Article title (≈60 characters)",
  "description": "One or two sentences for Google and social previews (≈150 characters).",
  "order": 8,
  "date": "2026-11-20",
  "updated": "2026-12-01",
  "keywords": ["main keyword", "related keyword"],
  "tldr": "The answer in two or three sentences. Shown in the 'In short' box and used by AI assistants.",
  "faq": [["Question?", "Answer."]]
}
-->
<p>First paragraph…</p>
<h2 id="section">Section</h2>
```

`updated` and `faq` are optional. Link to other posts with `../<slug>/` and to the landing with `../../#download`, `../../#pricing`.

On every deploy, `deploy/cloudflare/build.sh` regenerates `site/blog/`, `site/sitemap.xml`, `site/blog/feed.xml`, `site/llms.txt` and `site/llms-full.txt`, and puts the three newest posts on the landing page. Run it locally to preview:

```bash
deploy/cloudflare/build.sh
```

The social preview image of a post is `site/assets/og/<slug>.jpg` (1200×630). Without one, the link still works but shows no image, so add one for every new post.
