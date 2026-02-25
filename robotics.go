package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/n0remac/GoDom/auth"
	"github.com/n0remac/GoDom/database"
	. "github.com/n0remac/GoDom/html"
)

type App struct {
	name  string
	store *database.DocumentStore
	posts *PostStore
	auth  *auth.AuthApp
}

func Robotics(mux *http.ServeMux, store *database.DocumentStore, authApp *auth.AuthApp) {
	app := &App{
		name:  "robotics",
		store: store,
		posts: NewPostStore(store),
		auth:  authApp,
	}
	mux.HandleFunc("/"+app.name, app.roboticsPageHandler())
	app.mountPostRoutes(mux)
}

func (a *App) roboticsPageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		posts, err := a.posts.ListPosts(context.Background())
		if err != nil {
			http.Error(w, "failed to load posts", http.StatusInternalServerError)
			return
		}
		currentUser, isLoggedIn := a.currentUser(r)

		statusMessage := strings.TrimSpace(r.URL.Query().Get("status"))
		errorMessage := strings.TrimSpace(r.URL.Query().Get("error"))
		ServeNode(RoboticsPage(a.name, posts, statusMessage, errorMessage, currentUser, isLoggedIn))(w, r)
	}
}

func (a *App) currentUser(r *http.Request) (*auth.User, bool) {
	if a.auth == nil {
		return nil, false
	}
	return a.auth.CurrentUser(r)
}

func RoboticsPage(page string, posts []*Post, statusMessage, errorMessage string, currentUser *auth.User, isLoggedIn bool) *Node {
	subtitle := "Recent updates from the Orcas Makers robotics team."
	composer := Nil()
	feedback := Nil()
	accountLabel := Nil()
	if isLoggedIn {
		subtitle = "Create a post with text and optional images."
		composer = CreatePostForm(page)
		feedback = Feedback(page, statusMessage, errorMessage)
		if currentUser != nil {
			accountLabel = P(Class("text-sm text-base-content/70"), T("Signed in as "+currentUser.Email))
		}
	}

	return DefaultLayout(
		Div(Attrs(map[string]string{
			"class":      "flex flex-col items-center min-h-screen gap-6",
			"data-theme": "dark",
		}),
			NavBar(),
			Div(
				Class("w-full max-w-6xl px-4 py-6 space-y-6"),
				Div(
					Class("card bg-base-200 p-6 space-y-4"),
					H1(Class("text-2xl font-bold"), T("Robotics")),
					P(Class("text-base-content/70"), T(subtitle)),
					accountLabel,
					feedback,
					composer,
				),
				PostsFeed(page, posts, isLoggedIn),
			),
		),
	)
}

func PostsFeed(page string, posts []*Post, canCreate bool) *Node {
	cards := make([]*Node, 0, len(posts))
	for _, post := range posts {
		cards = append(cards, PostCard(page, post))
	}

	emptyText := "No posts yet."
	if canCreate {
		emptyText = "No posts yet. Create the first robotics post above."
	}
	if len(cards) == 0 {
		cards = append(cards,
			Div(
				Class("card bg-base-200 p-6 text-base-content/70"),
				T(emptyText),
			),
		)
	}

	return Div(
		Id(page+"-posts"),
		Class("space-y-4"),
		Ch(cards),
	)
}
