package httpapi

import (
	"net/http"
	"testing"
)

// Пробы. Смысл разделения в том, что живость и готовность отвечают
// на разные вопросы, и путать их дорого: моргнувшая база не должна
// перезапускать реплики, а останавливающаяся реплика обязана успеть
// сказать «мне больше не давайте» до того, как перестанет отвечать.

func TestProbesAnswerWithoutSession(t *testing.T) {
	a := newAPI(t)
	anon := a.session()
	for _, path := range []string{"/healthz", "/readyz"} {
		if code, raw := anon.do("GET", path, nil); code != http.StatusOK {
			t.Errorf("GET %s: код %d, тело %s", path, code, raw)
		}
	}
}

func TestDrainingReplicaStopsBeingReadyButStaysAlive(t *testing.T) {
	a := newAPI(t)
	anon := a.session()

	a.impl.Drain()

	// Готовность снята: балансировщик обязан вычеркнуть реплику.
	if code, _ := anon.do("GET", "/readyz", nil); code != http.StatusServiceUnavailable {
		t.Errorf("готовность останавливающейся реплики: код %d, ожидался 503", code)
	}
	// Живость — нет: перезапускать процесс, который штатно закрывается,
	// значит превратить выкладку в убийство посреди работы.
	if code, _ := anon.do("GET", "/healthz", nil); code != http.StatusOK {
		t.Errorf("живость останавливающейся реплики: код %d, ожидался 200", code)
	}

	// И запросы, которые уже пришли, по-прежнему обслуживаются: снятие
	// с балансировки — не отказ.
	owner := a.registerOrg("Останавливаемся")
	owner.mustDo("GET", "/api/boards", nil, http.StatusOK)
}
