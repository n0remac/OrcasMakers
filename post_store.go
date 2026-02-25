package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/n0remac/GoDom/database"
)

const postPrefix = "post:"

type Post struct {
	ID         string    `json:"id"`
	Page       string    `json:"page,omitempty"`
	Text       string    `json:"text"`
	ImagePaths []string  `json:"image_paths"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type PostStore struct {
	ds *database.DocumentStore
}

func NewPostStore(ds *database.DocumentStore) *PostStore {
	return &PostStore{ds: ds}
}

func (s *PostStore) CreatePost(ctx context.Context, page, postID, text string, imagePaths []string) (*Post, error) {
	if s == nil || s.ds == nil {
		return nil, errors.New("post store not initialized")
	}
	page = normalizePageName(page)
	if page == "" {
		return nil, errors.New("post page is required")
	}

	postID = strings.TrimSpace(postID)
	if postID == "" {
		id, err := generatePostID()
		if err != nil {
			return nil, err
		}
		postID = id
	}

	now := time.Now().UTC()
	post := &Post{
		ID:         postID,
		Page:       page,
		Text:       strings.TrimSpace(text),
		ImagePaths: append([]string(nil), imagePaths...),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if post.ImagePaths == nil {
		post.ImagePaths = []string{}
	}

	body, err := json.Marshal(post)
	if err != nil {
		return nil, err
	}
	if err := s.ds.Put(ctx, postDocumentID(postID), body); err != nil {
		return nil, err
	}
	return post, nil
}

func (s *PostStore) ListPosts(ctx context.Context) ([]*Post, error) {
	return s.listPosts(ctx, "")
}

func (s *PostStore) ListPostsByPage(ctx context.Context, page string) ([]*Post, error) {
	return s.listPosts(ctx, page)
}

func (s *PostStore) listPosts(ctx context.Context, page string) ([]*Post, error) {
	if s == nil || s.ds == nil {
		return nil, errors.New("post store not initialized")
	}
	page = normalizePageName(page)

	ids, err := s.ds.List(ctx)
	if err != nil {
		return nil, err
	}

	posts := make([]*Post, 0)
	for _, id := range ids {
		if !strings.HasPrefix(id, postPrefix) {
			continue
		}
		body, err := s.ds.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if body == nil {
			continue
		}

		var post Post
		if err := json.Unmarshal(body, &post); err != nil {
			return nil, err
		}
		if post.ID == "" {
			post.ID = strings.TrimPrefix(id, postPrefix)
		}
		if post.ImagePaths == nil {
			post.ImagePaths = []string{}
		}
		if page != "" && !postMatchesPage(&post, page) {
			continue
		}
		posts = append(posts, &post)
	}

	sort.Slice(posts, func(i, j int) bool {
		return posts[i].CreatedAt.After(posts[j].CreatedAt)
	})
	return posts, nil
}

func postMatchesPage(post *Post, page string) bool {
	if post == nil {
		return false
	}
	if page == "" {
		return true
	}
	return normalizePageName(post.Page) == page
}

func normalizePageName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func postDocumentID(postID string) string {
	return postPrefix + postID
}

func generatePostID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
