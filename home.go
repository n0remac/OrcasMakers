package main

import (
	"fmt"
	"net/http"

	. "github.com/n0remac/OrcasMakers/html"
	. "github.com/n0remac/OrcasMakers/websocket"
)

func Home(mux *http.ServeMux, websocketRegistry *CommandRegistry) {
	mux.HandleFunc("/", ServeNode(HomePage(websocketRegistry)))
}

func HomePage(websocketRegistry *CommandRegistry) *Node {
	id := "home"

	websocketRegistry.RegisterWebsocket("test", func(_ string, hub *Hub, data map[string]interface{}) {
		WsHub.Broadcast <- WebsocketMessage{
			Room: id,
			Content: []byte(
				Div(
					Id("test-message"),
					T("Received test message: "),
					Span(Class("font-bold"), T(fmt.Sprintf("%v", data["test"]))),
				).Render(),
			),
		}
	})

	return DefaultLayout(
		Attr("hx-ext", "ws"),
		Attr("ws-connect", "/ws/hub?room="+id),
		Div(Attrs(map[string]string{
			"class":      "flex flex-col items-center min-h-screen",
			"data-theme": "dark",
		}),
			NavBar(),
			Div(
				T("Welcome to the OrcasMakers Home Page!"),
			),
			Div(Id("test-message")),
			Form(
				Attr("ws-send", "submit"),
				Input(
					Type("hidden"),
					Name("type"),
					Value("test"),
				),
				Input(
					Type("hidden"),
					Name("test"),
					Value("test"),
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

func NavBar() *Node {
	return Nav(Class("bg-base-300 p-4 w-full"),
		Div(Class("container mx-auto flex justify-center"),
			Ul(Class("flex space-x-6"),
				Li(A(Href("/about"), T("About"))),
			),
		),
	)
}
