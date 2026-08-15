package httpapi

import (
	_ "embed"
	"net/http"
)

// Описание контракта машиночитаемым файлом, а не текстом в README.
//
// Текстовое описание расходится с кодом молча и обнаруживается тем, кто
// по нему что-то написал. Файл хотя бы можно скормить генератору клиента
// и увидеть расхождение сразу.
//
// Файл лежит рядом и встраивается в бинарник: у нас один образ, и
// описание обязано ехать вместе с той версией, которую описывает.

//go:embed openapi.json
var openapiDocument []byte

func (s *Server) registerContractRoutes(mux *http.ServeMux) {
	// Путь внутренний, без версии: обёртка versioned срежет её раньше,
	// чем маршрут дойдёт до мультиплексора. Снаружи это по-прежнему
	// /api/v1/openapi.json.
	//
	// Без ключа: описание — не данные организации, а обещание сервера.
	// Требовать вход, чтобы прочитать, как войти, — замкнутый круг.
	mux.HandleFunc("GET /api/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json; charset=utf-8")
		w.Header().Set("API-Version", Version)
		_, _ = w.Write(openapiDocument)
	})
}
