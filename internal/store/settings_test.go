package store

import (
	"reflect"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func TestKVRoundTripDoesNotLeakIntoSettings(t *testing.T) {
	s := newTestStore(t)
	var got map[string][]string
	if ok, err := s.GetKV("subproject_dismissed", &got); err != nil || ok {
		t.Fatalf("ausente: ok=%v err=%v", ok, err)
	}
	if err := s.PutKV("subproject_dismissed", map[string][]string{"1": {"apps/web"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutKV("subproject_dismissed", map[string][]string{"1": {"apps/web", "apps/api"}}); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.GetKV("subproject_dismissed", &got); err != nil || !ok || len(got["1"]) != 2 {
		t.Fatalf("got=%v ok=%v err=%v", got, ok, err)
	}
	st, err := s.GetSettings()
	if err != nil || !reflect.DeepEqual(st, domain.DefaultSettings()) {
		t.Fatalf("settings = %+v err=%v", st, err)
	}
}
