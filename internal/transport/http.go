package transport

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nebuk89/cdn_git/internal/model"
	"github.com/nebuk89/cdn_git/internal/node"
)

const maxRequestBytes = 64 << 20

type Server struct {
	node *node.Node
}

type objectEnvelope struct {
	ID      string            `json:"id"`
	Private bool              `json:"private"`
	Object  model.GraphObject `json:"object"`
}

type transitionEnvelope struct {
	Transition model.Transition  `json:"transition"`
	Receipt    model.EdgeReceipt `json:"receipt"`
}

type finalizeRequest struct {
	TransitionID string `json:"transition_id"`
}

type authorityResponse struct {
	Records []model.JournalRecord `json:"records"`
}

type refsResponse struct {
	Refs      map[string]string `json:"refs"`
	Divergent map[string]string `json:"divergent"`
}

func NewServer(node *node.Node) *Server {
	return &Server{node: node}
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v0/health", server.health)
	mux.HandleFunc("GET /v0/objects/{id}", server.auth(server.getObject))
	mux.HandleFunc("POST /v0/objects", server.auth(server.putObject))
	mux.HandleFunc("GET /v0/transitions/{id}", server.auth(server.getTransition))
	mux.HandleFunc("POST /v0/transitions", server.auth(server.putTransition))
	mux.HandleFunc("POST /v0/finalize", server.auth(server.finalize))
	mux.HandleFunc("GET /v0/authority", server.auth(server.authority))
	mux.HandleFunc("GET /v0/refs", server.auth(server.refs))
	return mux
}

func (server *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		want := "Bearer " + server.node.Domain().PeerToken
		got := request.Header.Get("Authorization")
		if len(got) != len(want) || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			writeError(response, http.StatusUnauthorized, errors.New("invalid peer credential"))
			return
		}
		next(response, request)
	}
}

func (server *Server) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, struct {
		Status      string `json:"status"`
		NodeID      string `json:"node_id"`
		TrustDomain string `json:"trust_domain"`
		Authority   bool   `json:"authority"`
	}{
		Status:      "ok",
		NodeID:      server.node.NodeID(),
		TrustDomain: server.node.Domain().ID,
		Authority:   server.node.IsAuthority(),
	})
}

func (server *Server) getObject(response http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	object, err := server.node.GetObject(id)
	if err != nil {
		writeError(response, http.StatusNotFound, err)
		return
	}
	writeJSON(response, http.StatusOK, objectEnvelope{
		ID:      id,
		Private: strings.HasPrefix(id, "priv:"),
		Object:  object,
	})
}

func (server *Server) putObject(response http.ResponseWriter, request *http.Request) {
	var envelope objectEnvelope
	if err := decodeJSON(response, request, &envelope); err != nil {
		writeError(response, http.StatusBadRequest, err)
		return
	}
	id, err := server.node.PutObject(envelope.Object, envelope.Private)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err)
		return
	}
	if id != envelope.ID {
		writeError(response, http.StatusConflict, errors.New("object ID does not match canonical content"))
		return
	}
	writeJSON(response, http.StatusCreated, struct {
		ID string `json:"id"`
	}{ID: id})
}

func (server *Server) getTransition(response http.ResponseWriter, request *http.Request) {
	transition, err := server.node.LoadTransition(request.PathValue("id"))
	if err != nil {
		writeError(response, http.StatusNotFound, err)
		return
	}
	receipt, err := server.node.ReceiptForTransition(transition.ID)
	if err != nil {
		writeError(response, http.StatusNotFound, err)
		return
	}
	writeJSON(response, http.StatusOK, transitionEnvelope{Transition: transition, Receipt: receipt})
}

func (server *Server) putTransition(response http.ResponseWriter, request *http.Request) {
	var envelope transitionEnvelope
	if err := decodeJSON(response, request, &envelope); err != nil {
		writeError(response, http.StatusBadRequest, err)
		return
	}
	if err := server.node.IngestTransition(envelope.Transition, envelope.Receipt); err != nil {
		writeError(response, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(response, http.StatusCreated, struct {
		TransitionID string `json:"transition_id"`
	}{TransitionID: envelope.Transition.ID})
}

func (server *Server) finalize(response http.ResponseWriter, request *http.Request) {
	var input finalizeRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, err)
		return
	}
	result, err := server.node.Finalize(input.TransitionID)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (server *Server) authority(response http.ResponseWriter, _ *http.Request) {
	records, err := server.node.AuthorityRecords()
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	writeJSON(response, http.StatusOK, authorityResponse{Records: records})
}

func (server *Server) refs(response http.ResponseWriter, _ *http.Request) {
	refs, divergent, err := server.node.Refs()
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)
		return
	}
	writeJSON(response, http.StatusOK, refsResponse{Refs: refs, Divergent: divergent})
}

func decodeJSON(response http.ResponseWriter, request *http.Request, destination any) error {
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request contains trailing JSON")
	}
	return nil
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(response http.ResponseWriter, status int, err error) {
	writeJSON(response, status, struct {
		Error string `json:"error"`
	}{Error: err.Error()})
}

type Client struct {
	httpClient *http.Client
	peerToken  string
}

func NewClient(peerToken string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		peerToken:  peerToken,
	}
}

func (client *Client) SyncTransition(ctx context.Context, local *node.Node, peerURL, transitionID string) error {
	return client.syncTransition(ctx, local, peerURL, transitionID, make(map[string]struct{}))
}

func (client *Client) syncTransition(
	ctx context.Context,
	local *node.Node,
	peerURL string,
	transitionID string,
	visited map[string]struct{},
) error {
	if _, ok := visited[transitionID]; ok {
		return nil
	}
	visited[transitionID] = struct{}{}
	transition, err := local.LoadTransition(transitionID)
	if err != nil {
		return err
	}
	for _, parentID := range transition.Body.ParentTransitions {
		if err := client.syncTransition(ctx, local, peerURL, parentID, visited); err != nil {
			return fmt.Errorf("replicate parent %s: %w", parentID, err)
		}
	}
	closure, err := local.VerifyObjectClosure(transition.Body.RequiredObjectManifest)
	if err != nil {
		return err
	}
	for _, id := range closure {
		object, err := local.GetObject(id)
		if err != nil {
			return err
		}
		envelope := objectEnvelope{
			ID:      id,
			Private: strings.HasPrefix(id, "priv:"),
			Object:  object,
		}
		if err := client.post(ctx, peerURL+"/v0/objects", envelope, nil); err != nil {
			return fmt.Errorf("replicate object %s: %w", id, err)
		}
	}
	receipt, err := local.ReceiptForTransition(transitionID)
	if err != nil {
		return err
	}
	if err := client.post(ctx, peerURL+"/v0/transitions", transitionEnvelope{
		Transition: transition,
		Receipt:    receipt,
	}, nil); err != nil {
		return fmt.Errorf("replicate transition: %w", err)
	}
	return nil
}

func (client *Client) Finalize(ctx context.Context, peerURL, transitionID string) (model.FinalizeResult, error) {
	var result model.FinalizeResult
	err := client.post(ctx, peerURL+"/v0/finalize", finalizeRequest{TransitionID: transitionID}, &result)
	return result, err
}

func (client *Client) PullAuthority(ctx context.Context, local *node.Node, peerURL string) error {
	var response authorityResponse
	if err := client.get(ctx, peerURL+"/v0/authority", &response); err != nil {
		return err
	}
	return local.ImportAuthorityRecords(response.Records)
}

func (client *Client) PeerRefs(ctx context.Context, peerURL string) (map[string]string, map[string]string, error) {
	var response refsResponse
	if err := client.get(ctx, peerURL+"/v0/refs", &response); err != nil {
		return nil, nil, err
	}
	return response.Refs, response.Divergent, nil
}

func (client *Client) get(ctx context.Context, endpoint string, destination any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	return client.do(request, destination)
}

func (client *Client) post(ctx context.Context, endpoint string, source, destination any) error {
	body, err := json.Marshal(source)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	return client.do(request, destination)
}

func (client *Client) do(request *http.Request, destination any) error {
	request.Header.Set("Authorization", "Bearer "+client.peerToken)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(response.Body).Decode(&failure); err != nil {
			return fmt.Errorf("peer returned %s", response.Status)
		}
		return fmt.Errorf("peer returned %s: %s", response.Status, failure.Error)
	}
	if destination == nil {
		_, err := io.Copy(io.Discard, response.Body)
		return err
	}
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		return err
	}
	return nil
}

func EscapeID(id string) string {
	return url.PathEscape(id)
}
