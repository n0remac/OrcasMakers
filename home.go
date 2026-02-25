package main

import (
	"fmt"
	"net/http"

	. "github.com/n0remac/GoDom/html"
	. "github.com/n0remac/GoDom/websocket"
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
				H1(T("Welcome to the Orcas Makers Home Page!")),
			),
			Div(
				T("We are an up and coming maker group on Orcas Island, WA. We have passions for robotics, wearable technology, and building quirky projects that create weirder and stronger communities."),
			),
			// Div(Id("test-message")),
			// Form(
			// 	Attr("ws-send", "submit"),
			// 	Input(
			// 		Type("hidden"),
			// 		Name("type"),
			// 		Value("test"),
			// 	),
			// 	Input(
			// 		Type("hidden"),
			// 		Name("test"),
			// 		Value("test"),
			// 	),
			// 	Input(
			// 		Type("submit"),
			// 		Class("btn btn-primary w-32"),
			// 		Value("Test Websocket"),
			// 	),
			// ),
		),
	)
}

func NavBar() *Node {
	return Nav(Class("bg-base-300 p-4 w-full"),
		Div(Class("container mx-auto flex justify-center"),
			Ul(Class("flex space-x-6"),
				Li(A(Href("/robotics"), T("Robotics"))),
				Li(A(Href("/software"), T("Software"))),
				Li(A(Href("/art"), T("Art"))),
			),
			Div(Class("flex-1")),
			Div(Class("flex items-center gap-4"),
				A(Href("/login"), Class("btn btn-sm"), T("login")),
			),
		),
	)
}
