package export

import (
	"errors"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pserrano95/pinacoteca/internal/i18n"
	"github.com/pserrano95/pinacoteca/internal/store"
)

// ExportArtist is an alias for Artist.
func ExportArtist(st *store.Store, artistSlug, outDir, lang string) error {
	return Artist(st, artistSlug, outDir, lang)
}

// Artist exports all artworks of the given artist from st to outDir,
// producing an archive that can be browsed without Pinacoteca.
func Artist(st *store.Store, artistSlug, outDir, lang string) error {
	artistSlug = strings.TrimSpace(artistSlug)
	if artistSlug == "" {
		return fmt.Errorf("artist slug is required")
	}
	outDir = strings.TrimSpace(outDir)
	if outDir == "" {
		return fmt.Errorf("output directory is required")
	}

	canData, err := canonicalPath(st.DataDir)
	if err != nil {
		return fmt.Errorf("data dir: %w", err)
	}
	canOut, err := canonicalPath(outDir)
	if err != nil {
		return fmt.Errorf("out dir: %w", err)
	}

	inside, err := isInsideOrSame(canData, canOut)
	if err != nil {
		return fmt.Errorf("check paths: %w", err)
	}
	if inside {
		return fmt.Errorf("output directory %q cannot be inside data directory %q", outDir, st.DataDir)
	}

	info, err := os.Stat(outDir)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("output path %q exists and is not a directory", outDir)
		}
		entries, err := os.ReadDir(outDir)
		if err != nil {
			return fmt.Errorf("read output directory: %w", err)
		}
		if len(entries) > 0 {
			return fmt.Errorf("output directory %q is not empty", outDir)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat output directory: %w", err)
	}

	artist, err := st.LoadArtist(artistSlug)
	if err != nil {
		return fmt.Errorf("load artist %q: %w", artistSlug, err)
	}

	allArtworks, err := st.WalkArtworks()
	if err != nil {
		return fmt.Errorf("walk artworks: %w", err)
	}

	var artworks []store.ArtworkRecord
	for _, art := range allArtworks {
		if art.ArtistSlug == artistSlug {
			artworks = append(artworks, art)
		}
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	artistSrc := filepath.Join(st.ArtistsRoot(), artist.Slug, store.ArtistFile)
	artistDst := filepath.Join(outDir, store.ArtistFile)
	if err := copyFile(artistSrc, artistDst); err != nil {
		return fmt.Errorf("copy artist.json: %w", err)
	}

	type exportItem struct {
		Title       string
		Date        string
		AgeYears    int
		Description string
		Tags        []string
		TagsString  string
		OriginalRel string
		ThumbRel    string
	}

	var items []exportItem
	for _, art := range artworks {
		artDstDir := filepath.Join(outDir, art.Folder)
		if err := os.MkdirAll(artDstDir, 0o755); err != nil {
			return fmt.Errorf("create artwork directory %q: %w", art.Folder, err)
		}

		origDst := filepath.Join(artDstDir, art.OriginalName)
		if err := copyFile(art.OriginalPath, origDst); err != nil {
			return fmt.Errorf("copy original %q: %w", art.Folder, err)
		}

		artJSONSrc := filepath.Join(art.Dir, store.ArtworkFile)
		artJSONDst := filepath.Join(artDstDir, store.ArtworkFile)
		if err := copyFile(artJSONSrc, artJSONDst); err != nil {
			return fmt.Errorf("copy artwork.json %q: %w", art.Folder, err)
		}

		var thumbRel string
		if art.ThumbPath != "" {
			if _, err := os.Stat(art.ThumbPath); err == nil {
				derivedDst := filepath.Join(artDstDir, store.DerivedDir)
				if err := os.MkdirAll(derivedDst, 0o755); err != nil {
					return fmt.Errorf("create derived directory %q: %w", art.Folder, err)
				}
				thumbDst := filepath.Join(derivedDst, store.ThumbName)
				if err := copyFile(art.ThumbPath, thumbDst); err != nil {
					return fmt.Errorf("copy thumbnail %q: %w", art.Folder, err)
				}
				thumbRel = filepath.ToSlash(filepath.Join(art.Folder, store.DerivedDir, store.ThumbName))
			}
		}

		origRel := filepath.ToSlash(filepath.Join(art.Folder, art.OriginalName))

		items = append(items, exportItem{
			Title:       displayTitle(art.Meta.Title),
			Date:        art.Meta.Date.Format("2006-01-02"),
			AgeYears:    art.AgeYears,
			Description: art.Meta.Description,
			Tags:        art.Meta.Tags,
			TagsString:  strings.Join(art.Meta.Tags, ", "),
			OriginalRel: origRel,
			ThumbRel:    thumbRel,
		})
	}

	lang = i18n.NormalizeLang(lang)
	tFunc := func(key string) string {
		return i18n.T(lang, key, nil)
	}
	ageLabelFunc := func(age int) string {
		return i18n.T(lang, "export_age_years", map[string]string{"age": strconv.Itoa(age)})
	}

	tmpl, err := template.New("index").Funcs(template.FuncMap{
		"T":        tFunc,
		"AgeLabel": ageLabelFunc,
	}).Parse(indexTemplateHTML)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	htmlDst := filepath.Join(outDir, "index.html")
	f, err := os.Create(htmlDst)
	if err != nil {
		return fmt.Errorf("create index.html: %w", err)
	}
	defer f.Close()

	tmplData := struct {
		Lang     string
		Title    string
		Artist   *store.Artist
		Artworks []exportItem
		T        func(key string) string
		AgeLabel func(age int) string
	}{
		Lang:     lang,
		Title:    i18n.T(lang, "export_title", map[string]string{"name": artist.Name}),
		Artist:   artist,
		Artworks: items,
		T:        tFunc,
		AgeLabel: ageLabelFunc,
	}

	if err := tmpl.Execute(f, tmplData); err != nil {
		return fmt.Errorf("render index.html: %w", err)
	}

	return nil
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	cur := abs
	var suffix []string
	for {
		realCur, err := filepath.EvalSymlinks(cur)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				realCur = filepath.Join(realCur, suffix[i])
			}
			return filepath.Clean(realCur), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return filepath.Clean(abs), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return filepath.Clean(abs), nil
		}
		suffix = append(suffix, filepath.Base(cur))
		cur = parent
	}
}

func isInsideOrSame(parent, sub string) (bool, error) {
	rel, err := filepath.Rel(parent, sub)
	if err != nil {
		return false, err
	}
	if rel == "." {
		return true, nil
	}
	if !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
		return true, nil
	}
	return false, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func displayTitle(t string) string {
	if strings.TrimSpace(t) == "" {
		return "—"
	}
	return t
}

const indexTemplateHTML = `<!DOCTYPE html>
<html lang="{{.Lang}}">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    body {
      font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      line-height: 1.5;
      color: #222;
      max-width: 800px;
      margin: 2rem auto;
      padding: 0 1rem;
    }
    h1 {
      margin-bottom: 2rem;
      padding-bottom: 0.5rem;
      border-bottom: 2px solid #eee;
    }
    .empty {
      color: #666;
      font-style: italic;
    }
    .artwork {
      margin-bottom: 3rem;
      padding-bottom: 2rem;
      border-bottom: 1px solid #eee;
    }
    .artwork h2 {
      margin: 0 0 0.5rem 0;
    }
    .artwork .age-line {
      color: #666;
      margin: 0 0 1rem 0;
      font-size: 0.95em;
    }
    .artwork img {
      max-width: 100%;
      height: auto;
      border-radius: 4px;
      display: block;
      margin-bottom: 1rem;
    }
    dl {
      display: grid;
      grid-template-columns: max-content 1fr;
      gap: 0.5rem 1rem;
      margin: 1rem 0 0 0;
    }
    dt {
      font-weight: 600;
      color: #555;
    }
    dd {
      margin: 0;
    }
    .tag {
      display: inline-block;
      background: #f0f0f0;
      padding: 0.15rem 0.5rem;
      border-radius: 3px;
      margin-right: 0.25rem;
      font-size: 0.85em;
    }
  </style>
</head>
<body>
  <main>
    <h1>{{.Title}}</h1>
    {{if not .Artworks}}
      <p class="empty">{{call .T "export_empty"}}</p>
    {{else}}
      {{range .Artworks}}
      <article class="artwork">
        <h2>{{.Title}}</h2>
        <p class="age-line">{{call $.AgeLabel .AgeYears}}</p>
        {{if .ThumbRel}}
        <a href="{{.OriginalRel}}"><img src="{{.ThumbRel}}" alt="{{.Title}}"></a>
        {{else}}
        <p><a href="{{.OriginalRel}}">{{call $.T "export_original"}}</a></p>
        {{end}}
        <dl>
          <dt>{{call $.T "export_date"}}</dt><dd>{{.Date}}</dd>
          <dt>{{call $.T "export_age"}}</dt><dd>{{.AgeYears}}</dd>
          {{if .Description}}
          <dt>{{call $.T "export_description"}}</dt><dd>{{.Description}}</dd>
          {{end}}
          {{if .Tags}}
          <dt>{{call $.T "export_tags"}}</dt>
          <dd>
            <span class="tags">{{range $i, $t := .Tags}}{{if $i}}, {{end}}<span class="tag">{{$t}}</span>{{end}}</span>
          </dd>
          {{end}}
        </dl>
      </article>
      {{end}}
    {{end}}
  </main>
</body>
</html>
`
