package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	. "github.com/n0remac/GoDom/html"
)

const (
	maxUploadSizeMB  = 32
	maxImageSizeMB   = 8
	maxImagesPerPost = 10
	maxTextLength    = 5000
)

func (a *App) mountPostRoutes(mux *http.ServeMux) {
	mux.HandleFunc(fmt.Sprintf("/%s/posts", a.name), a.createPostHandler())
}

func (a *App) createPostHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if _, ok := a.currentUser(r); !ok {
			a.respondUnauthorized(w, r)
			return
		}

		ctx := context.Background()
		_, err := a.createPostFromRequest(ctx, w, r)
		if err != nil {
			a.respondPostError(w, r, err)
			return
		}

		if isHTMX(r) {
			posts, err := a.posts.ListPosts(ctx)
			if err != nil {
				a.respondPostError(w, r, errors.New("post created, but feed reload failed"))
				return
			}
			writeHTML(w, http.StatusCreated,
				FeedbackOOB(a.name, "Post created successfully.", false).Render()+
					PostsFeed(a.name, posts, true).Render(),
			)
			return
		}

		http.Redirect(w, r, "/"+a.name+"?status="+url.QueryEscape("post created"), http.StatusSeeOther)
	}
}

func (a *App) respondUnauthorized(w http.ResponseWriter, r *http.Request) {
	if isHTMX(r) {
		writeHTML(w, http.StatusUnauthorized, FeedbackOOB(a.name, "Please log in to create a post.", true).Render())
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) createPostFromRequest(ctx context.Context, w http.ResponseWriter, r *http.Request) (*Post, error) {
	maxBytes := int64(maxUploadSizeMB) * 1024 * 1024
	maxImageBytes := int64(maxImageSizeMB) * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		return nil, errors.New("upload failed: request too large or invalid multipart data")
	}

	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" {
		return nil, errors.New("post text is required")
	}
	if len(text) > maxTextLength {
		return nil, fmt.Errorf("post text must be %d characters or less", maxTextLength)
	}

	imageHeaders := r.MultipartForm.File["images"]
	if len(imageHeaders) == 0 {
		return nil, errors.New("please upload at least one image")
	}
	if len(imageHeaders) > maxImagesPerPost {
		return nil, fmt.Errorf("you can upload up to %d images per post", maxImagesPerPost)
	}

	postID, err := generatePostID()
	if err != nil {
		return nil, errors.New("failed to initialize post")
	}

	savedPaths := make([]string, 0, len(imageHeaders))
	for i, header := range imageHeaders {
		if header.Size > maxImageBytes {
			a.cleanupImages(ctx, savedPaths)
			return nil, fmt.Errorf("each image must be %dMB or smaller", maxImageSizeMB)
		}

		file, err := header.Open()
		if err != nil {
			a.cleanupImages(ctx, savedPaths)
			return nil, errors.New("failed to read uploaded image")
		}

		storedPath := buildImagePath(postID, header.Filename, a.name, i)
		record, storeErr := a.store.UploadImage(ctx, storedPath, file, header.Header.Get("Content-Type"))
		_ = file.Close()
		if storeErr != nil {
			a.cleanupImages(ctx, savedPaths)
			return nil, errors.New("failed to store uploaded image")
		}
		if record == nil || !strings.HasPrefix(record.ContentType, "image/") {
			if record != nil {
				_ = a.store.DeleteImage(ctx, record.Path)
			}
			a.cleanupImages(ctx, savedPaths)
			return nil, errors.New("all uploaded files must be images")
		}

		savedPaths = append(savedPaths, record.Path)
	}

	post, err := a.posts.CreatePost(ctx, postID, text, savedPaths)
	if err != nil {
		a.cleanupImages(ctx, savedPaths)
		return nil, errors.New("failed to save post")
	}
	return post, nil
}

func (a *App) cleanupImages(ctx context.Context, paths []string) {
	for _, imagePath := range paths {
		_ = a.store.DeleteImage(ctx, imagePath)
	}
}

func (a *App) respondPostError(w http.ResponseWriter, r *http.Request, err error) {
	if isHTMX(r) {
		writeHTML(w, http.StatusBadRequest, FeedbackOOB(a.name, err.Error(), true).Render())
		return
	}
	http.Redirect(w, r, "/"+a.name+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
}

func CreatePostForm(page string) *Node {
	return Form(
		Method("POST"),
		Action("/"+page+"/posts"),
		Attr("enctype", "multipart/form-data"),
		Attr("hx-post", "/"+page+"/posts"),
		Attr("hx-encoding", "multipart/form-data"),
		Attr("hx-target", "#"+page+"-posts"),
		Attr("hx-swap", "outerHTML"),
		Attr("hx-on::after-request", "if(event.detail.successful){ this.reset(); }"),
		Class("space-y-3"),
		Div(
			Class("space-y-2"),
			Label(For(page+"-text"), Class("font-medium"), T("Post Text")),
			TextArea(
				Id(page+"-text"),
				Name("text"),
				Rows(5),
				Attr("required", "required"),
				Placeholder("Enter your post text here..."),
				Class("textarea textarea-bordered w-full"),
			),
		),
		Div(
			Class("space-y-2"),
			Label(For(page+"-images"), Class("font-medium"), T("Images")),
			Input(
				Id(page+"-images"),
				Type("file"),
				Name("images"),
				Attr("accept", "image/*"),
				Attr("multiple", "multiple"),
				Attr("required", "required"),
				Class("file-input file-input-bordered w-full"),
			),
			P(
				Class("text-xs text-base-content/70"),
				T(fmt.Sprintf("Upload up to %d images, %dMB each.", maxImagesPerPost, maxImageSizeMB)),
			),
		),
		Button(
			Type("submit"),
			Class("btn btn-primary"),
			T("Create Post"),
		),
	)
}

func FeedbackOOB(page, message string, isError bool) *Node {
	klass := "alert alert-success"
	if isError {
		klass = "alert alert-error"
	}
	return Div(
		Id(page+"-feedback"),
		Attr("hx-swap-oob", "outerHTML"),
		Class(klass),
		T(message),
	)
}

func isHTMX(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("HX-Request"), "true")
}

func writeHTML(w http.ResponseWriter, status int, content string) {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(content))
}

func buildImagePath(postID, filename, page string, index int) string {
	base := filepath.Base(strings.TrimSpace(filename))
	if base == "" || base == "." {
		base = fmt.Sprintf("image-%d", index+1)
	}

	ext := strings.ToLower(filepath.Ext(base))
	name := strings.TrimSuffix(base, ext)
	slug := slugFilename(name)
	if slug == "" {
		slug = fmt.Sprintf("image-%d", index+1)
	}
	if ext == "" {
		ext = ".img"
	}

	return fmt.Sprintf("%s/%s/%d-%s%s", page, postID, time.Now().UnixNano(), slug, ext)
}

func slugFilename(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return ""
	}

	var b strings.Builder
	lastDash := false
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}

	out := strings.Trim(b.String(), "-")
	if out == "" {
		return ""
	}
	return out
}

func PostCard(page string, post *Post) *Node {
	images := make([]*Node, 0, len(post.ImagePaths))
	for _, imagePath := range post.ImagePaths {
		images = append(images,
			Div(
				Class("overflow-hidden rounded-lg bg-base-300"),
				Img(
					Src("/images/"+imagePath),
					Alt(page+" post image"),
					Class("h-56 w-full object-cover"),
				),
			),
		)
	}

	return Div(
		Class("card bg-base-200 p-6 space-y-4"),
		P(Class("whitespace-pre-wrap"), T(post.Text)),
		Div(Class("grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3"), Ch(images)),
		P(
			Class("text-xs text-base-content/60"),
			T("Posted "+post.CreatedAt.Local().Format(time.RFC1123)),
		),
	)
}

func Feedback(page string, statusMessage, errorMessage string) *Node {
	if strings.TrimSpace(errorMessage) != "" {
		return Div(
			Id(page+"-feedback"),
			Class("alert alert-error"),
			T(errorMessage),
		)
	}
	if strings.TrimSpace(statusMessage) != "" {
		return Div(
			Id(page+"-feedback"),
			Class("alert alert-success"),
			T(statusMessage),
		)
	}
	return Div(Id(page+"-feedback"))
}
