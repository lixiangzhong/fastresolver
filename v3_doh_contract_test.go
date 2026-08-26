package fastresolver_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	fastresolver "github.com/lixiangzhong/fastresolver/v3"
	"github.com/miekg/dns"
)

func TestV3_DoHUsesZeroWireIDAndRestoresCallerID(t *testing.T) {
	requestIDs := make(chan uint16, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		wireQuery, err := base64.RawURLEncoding.DecodeString(request.URL.Query().Get("dns"))
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		query := new(dns.Msg)
		if err = query.Unpack(wireQuery); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		requestIDs <- query.Id
		response := new(dns.Msg).SetReply(query)
		wireResponse, err := response.Pack()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/dns-message")
		_, _ = writer.Write(wireResponse)
	}))
	defer server.Close()

	response, err := fastresolver.NewDoH(server.URL, time.Second).Lookup(
		context.Background(),
		"doh.example",
		dns.TypeA,
	)
	if err != nil {
		t.Fatal(err)
	}
	if wireID := <-requestIDs; wireID != 0 {
		t.Fatalf("got wire query ID %d, want 0", wireID)
	}
	if response.Id == 0 {
		t.Fatal("caller-facing response ID was not restored")
	}
}
