package e2e

import (
	"context"
	"fmt"
)

type knownFailure struct {
	Journey string
	Step    string
	Reason  string
	Issue   string
}

// CONOCIDOS lists assertion failures reviewed by the repository owner.
// Step is the exact Step.What label within Journey.
var CONOCIDOS = []knownFailure{}

type screenSpec struct {
	name  string
	path  func(h *Harness) string
	title string
}

var screens = []screenSpec{
	{"gallery", func(h *Harness) string { return "/" }, "Gallery"},
	{"new-gallery", func(h *Harness) string { return "/galleries/new" }, "New gallery"},
	{"manage-gallery", func(h *Harness) string { return "/galleries/" + h.GallerySlug }, "Family Room"},
	{"gallery-room", func(h *Harness) string { return "/galleries/" + h.GallerySlug + "/room" }, "Family Room"},
	{"room-viewer", func(h *Harness) string {
		return fmt.Sprintf("/galleries/%s/room/%s/%s", h.GallerySlug, h.ArtistSlug, h.Artwork1Folder)
	}, "Sun and House"},
	{"new-artist", func(h *Harness) string { return "/artists/new" }, "New artist"},
	{"upload-artwork", func(h *Harness) string { return "/artworks/new" }, "Upload artwork"},
	{"artwork-detail", func(h *Harness) string {
		return fmt.Sprintf("/artists/%s/artworks/%s", h.ArtistSlug, h.Artwork1Folder)
	}, "Sun and House"},
	{"guardian-galleries", func(h *Harness) string {
		return fmt.Sprintf("/artists/%s/galleries", h.ArtistSlug)
	}, "Sofia"},
}

func screenSteps(prefix string, maxWidth int) []Step {
	missingScreen := HTTPFailure{Path: "/this-screen-does-not-exist", Status: 404}
	steps := make([]Step, 0, len(screens)+2)
	steps = append(steps, Step{
		What: fmt.Sprintf("%sOpen a route that does not exist and get 404 (not 200)", prefix),
		Do: func(ctx context.Context, h *Harness) error {
			return ExpectStatus(ctx, h, missingScreen.Path, int(missingScreen.Status))
		},
		ExpectedHTTP: &missingScreen,
	})
	for _, sc := range screens {
		sc := sc
		steps = append(steps, Step{
			What: fmt.Sprintf("%sOpen %q and see its title (%s) without horizontal overflow", prefix, sc.name, sc.title),
			Do: func(ctx context.Context, h *Harness) error {
				if err := Navigate(ctx, h, sc.path(h)); err != nil {
					return err
				}
				if err := SeeText(ctx, "main h1", sc.title); err != nil {
					return err
				}
				return NoOverflow(ctx, sc.name, maxWidth)
			},
		})
	}
	steps = append(steps, Step{
		What: fmt.Sprintf("%sLog out, open \"login\" and see \"Sign in\" without horizontal overflow", prefix),
		Do: func(ctx context.Context, h *Harness) error {
			if err := Click(ctx, `form[action="/logout"] button[type="submit"]`); err != nil {
				return err
			}
			if err := SeeText(ctx, "main h1", "Sign in"); err != nil {
				return err
			}
			return NoOverflow(ctx, "login", maxWidth)
		},
	})
	return steps
}

// Journeys is the complete map of user journeys in Pinacoteca.
var Journeys = []Journey{
	{
		ID:       "screens",
		What:     "Open every screen by its title on desktop without horizontal overflow",
		Start:    "/",
		Viewport: ViewportDesktop,
		Steps:    screenSteps("", 1280),
	},
	{
		ID:       "screens-mobile",
		What:     "Open every screen at 390 px without horizontal overflow",
		Start:    "/",
		Viewport: ViewportMobile,
		Steps:    screenSteps("At 390 px: ", 390),
	},
	{
		ID:       "view-artwork",
		What:     "Open an artwork from the gallery and see its image and metadata",
		Start:    "/",
		Viewport: ViewportDesktop,
		Steps: []Step{
			{
				What: "See Sun and House in the gallery grid",
				Do:   func(ctx context.Context, h *Harness) error { return SeeText(ctx, ".grid", "Sun and House") },
			},
			{
				What: "Click the Sun and House tile",
				Do: func(ctx context.Context, h *Harness) error {
					sel := fmt.Sprintf(`a.tile[href^="/artists/%s/artworks/%s"]`, h.ArtistSlug, h.Artwork1Folder)
					return Click(ctx, sel)
				},
			},
			{
				What: "See the artwork title, loaded image, and gifted-to metadata",
				Do: func(ctx context.Context, h *Harness) error {
					if err := SeeText(ctx, "article.artwork h1", "Sun and House"); err != nil {
						return err
					}
					if err := ImageLoaded(ctx, "article.artwork img"); err != nil {
						return err
					}
					return SeeText(ctx, "article.artwork dl", "Abuela")
				},
			},
		},
	},
	{
		ID:       "switch-language",
		What:     "Switch the interface language between English and Spanish",
		Start:    "/",
		Viewport: ViewportDesktop,
		Steps: []Step{
			{
				What: "See the gallery heading in English",
				Do: func(ctx context.Context, h *Harness) error {
					if err := SeeText(ctx, "main h1", "Gallery"); err != nil {
						return err
					}
					return AttrEquals(ctx, "html", "lang", "en")
				},
			},
			{
				What: "Click Español",
				Do:   func(ctx context.Context, h *Harness) error { return Click(ctx, `a[href="?lang=es"]`) },
			},
			{
				What: "See the gallery heading in Spanish",
				Do: func(ctx context.Context, h *Harness) error {
					if err := SeeText(ctx, "main h1", "Galería"); err != nil {
						return err
					}
					return AttrEquals(ctx, "html", "lang", "es")
				},
			},
			{
				What: "Click English",
				Do:   func(ctx context.Context, h *Harness) error { return Click(ctx, `a[href="?lang=en"]`) },
			},
			{
				What: "See the gallery heading back in English",
				Do: func(ctx context.Context, h *Harness) error {
					if err := SeeText(ctx, "main h1", "Gallery"); err != nil {
						return err
					}
					return AttrEquals(ctx, "html", "lang", "en")
				},
			},
		},
	},
	{
		ID:       "room-viewer",
		What:     "Enter a gallery room, step to the next artwork in the viewer, and return to the room",
		Start:    "/",
		Viewport: ViewportDesktop,
		Steps: []Step{
			{
				What: "Click the Family Room link",
				Do: func(ctx context.Context, h *Harness) error {
					return Click(ctx, fmt.Sprintf(`nav.room-links a[href^="/galleries/%s/room"]`, h.GallerySlug))
				},
			},
			{
				What: "See the exhibition room with both artworks",
				Do: func(ctx context.Context, h *Harness) error {
					if err := See(ctx, "main.gallery-room"); err != nil {
						return err
					}
					if err := SeeText(ctx, ".salon", "Blue Dragon"); err != nil {
						return err
					}
					return SeeText(ctx, ".salon", "Sun and House")
				},
			},
			{
				What: "Open the first exhibit in the viewer",
				Do:   func(ctx context.Context, h *Harness) error { return Click(ctx, ".salon a.exhibit") },
			},
			{
				What: "See the first artwork in the room viewer",
				Do: func(ctx context.Context, h *Harness) error {
					if err := See(ctx, "main.room-viewer"); err != nil {
						return err
					}
					return SeeText(ctx, "main.room-viewer h1", "Blue Dragon")
				},
			},
			{
				What: "Click Next to view the second artwork",
				Do:   func(ctx context.Context, h *Harness) error { return Click(ctx, "a.viewer-next") },
			},
			{
				What: "See Sun and House in the room viewer",
				Do: func(ctx context.Context, h *Harness) error {
					if err := SeeText(ctx, "main.room-viewer h1", "Sun and House"); err != nil {
						return err
					}
					return ImageLoaded(ctx, ".viewer-stage img")
				},
			},
			{
				What: "Click Back to the room and see the salon wall again",
				Do: func(ctx context.Context, h *Harness) error {
					if err := Click(ctx, "a.viewer-back"); err != nil {
						return err
					}
					return See(ctx, "main.gallery-room")
				},
			},
		},
	},
	{
		ID:       "upload-artwork",
		What:     "Upload a new drawing and see it appear in the gallery",
		Start:    "/artworks/new",
		Viewport: ViewportDesktop,
		Steps: []Step{
			{
				What: "See the upload form",
				Do:   func(ctx context.Context, h *Harness) error { return SeeText(ctx, "main h1", "Upload artwork") },
			},
			{
				What: "Select artist Sofia, attach a drawing, and fill title and date",
				Do: func(ctx context.Context, h *Harness) error {
					if err := Select(ctx, `select[name="artist_slug"]`, h.ArtistSlug); err != nil {
						return err
					}
					if err := UploadFile(ctx, `input[name="image"]`, h.SampleImagePath); err != nil {
						return err
					}
					if err := Fill(ctx, `input[name="title"]`, "Rainbow Cat"); err != nil {
						return err
					}
					return Fill(ctx, `input[name="date"]`, "2025-05-01")
				},
			},
			{
				What: "Submit the upload form",
				Do: func(ctx context.Context, h *Harness) error {
					return Click(ctx, `form[action="/artworks/new"] button[type="submit"]`)
				},
			},
			{
				What: "See Rainbow Cat in the gallery",
				Do: func(ctx context.Context, h *Harness) error {
					if err := See(ctx, ".grid"); err != nil {
						return err
					}
					return SeeText(ctx, ".grid", "Rainbow Cat")
				},
			},
		},
	},
	{
		ID:       "guardian-visibility",
		What:     "Hide an artwork as guardian, verify it disappears from the gallery, then show it again",
		Start:    "/",
		Viewport: ViewportDesktop,
		Steps: []Step{
			{
				What: "Open Sofia's guardian page",
				Do: func(ctx context.Context, h *Harness) error {
					return Click(ctx, fmt.Sprintf(`a[href^="/artists/%s/galleries"]`, h.ArtistSlug))
				},
			},
			{
				What: "Hide Sun and House",
				Do: func(ctx context.Context, h *Harness) error {
					sel := fmt.Sprintf(`form[action="/artists/%s/artworks/%s/visibility"] button[type="submit"]`, h.ArtistSlug, h.Artwork1Folder)
					if err := Click(ctx, sel); err != nil {
						return err
					}
					return SeeText(ctx, sel, "Show")
				},
			},
			{
				What: "Open the gallery as a viewer and verify Sun and House is hidden",
				Do: func(ctx context.Context, h *Harness) error {
					if err := SetSession(ctx, h, h.ViewerSessionCookie); err != nil {
						return err
					}
					if err := Navigate(ctx, h, "/"); err != nil {
						return err
					}
					if err := NotSeeText(ctx, ".grid", "Sun and House"); err != nil {
						return err
					}
					return SeeText(ctx, ".grid", "Blue Dragon")
				},
			},
			{
				What: "Return to Sofia's guardian page and show Sun and House again",
				Do: func(ctx context.Context, h *Harness) error {
					if err := SetSession(ctx, h, h.SessionCookie); err != nil {
						return err
					}
					if err := Navigate(ctx, h, fmt.Sprintf("/artists/%s/galleries", h.ArtistSlug)); err != nil {
						return err
					}
					sel := fmt.Sprintf(`form[action="/artists/%s/artworks/%s/visibility"] button[type="submit"]`, h.ArtistSlug, h.Artwork1Folder)
					if err := Click(ctx, sel); err != nil {
						return err
					}
					return SeeText(ctx, sel, "Hide")
				},
			},
			{
				What: "Open the gallery as a viewer and verify Sun and House is visible again",
				Do: func(ctx context.Context, h *Harness) error {
					if err := SetSession(ctx, h, h.ViewerSessionCookie); err != nil {
						return err
					}
					if err := Navigate(ctx, h, "/"); err != nil {
						return err
					}
					return SeeText(ctx, ".grid", "Sun and House")
				},
			},
		},
	},
}
