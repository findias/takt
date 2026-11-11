module github.com/findias/takt

go 1.26.11

// Версия языка — она же нижняя граница тулчейна, и поднимается она
// не за новыми возможностями, а по разбору уязвимостей: govulncheck
// 23.08.2026 нашёл семь достижимых дыр в стандартной библиотеке
// 1.26.4 — net/url, net/http, crypto/tls, encoding/xml, encoding/asn1.
// Все закрыты в 1.26.6. Сборка на более старом тулчейне теперь
// откажется, и это правильнее, чем собраться молча.
//
// 23.10.2026 — снова: одиннадцать бюллетеней к 1.26.6 (net/http,
// crypto/tls, crypto/x509, mime/multipart; GO-2026-6603…6617, 6809),
// закрыты в 1.26.9, последний — в 1.26.10. mime/multipart теперь
// на пути загрузки вложений.
//
// 11.11.2026 — GO-2026-6859: гонка заголовков в net/http при
// превышении MaxBytesReader могла уронить HTTP/1-сервер. Нижняя
// граница 1.26.11 не даёт собрать приложение с уязвимым net/http.

require (
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.11.0
	github.com/pb33f/libopenapi v0.41.8
	github.com/pb33f/libopenapi-validator v0.16.2
	go.yaml.in/yaml/v4 v4.0.0-rc.6
	golang.org/x/crypto v0.58.0
	golang.org/x/text v0.43.0
)

require (
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/basgys/goxml2json v1.1.1-0.20231018121955-e66ee54ceaad // indirect
	github.com/buger/jsonparser v1.6.1 // indirect
	github.com/go-openapi/jsonpointer v1.0.2 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/pb33f/go-yaml v0.1.1 // indirect
	github.com/pb33f/jsonpath v0.8.5 // indirect
	github.com/pb33f/jsonschema/v6 v6.0.3 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.2 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
)
