package main

import (
	"net/http"

	. "github.com/n0remac/GoDom/html"
)

func Robotics(mux *http.ServeMux) {
	mux.HandleFunc("/robotics", ServeNode(RoboticsPage()))
}

func RoboticsPage() *Node {
	id := "robotics"

	return DefaultLayout(
		Attr("hx-ext", "ws"),
		Attr("ws-connect", "/ws/hub?room="+id),
		Div(Attrs(map[string]string{
			"class":      "flex flex-col items-center min-h-screen",
			"data-theme": "dark",
		}),
			Div(
				T("Welcome to the Orcas Makers Robotics Page!"),
			),
			Div(Id("test-message")),
			Form(
				Attr("ws-send", "submit"),
				Input(
					Type("hidden"),
					Name("type"),
					Value("add-content"),
				),
				Input(
					Name("test"),
					Value("Enter some content to add to the page"),
				),
				Input(
					Type("submit"),
					Class("btn btn-primary w-32"),
					Value("Test Websocket"),
				),
			),
		),
	)
}
