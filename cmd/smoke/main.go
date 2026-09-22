package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const baseURL = "http://localhost:8080/api/v1"

type apiClient struct{ http *http.Client }
type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}
type cell struct {
	SlotID, Label, Type, Status string
	Occupant                    *struct{ UserID, FullName, VehicleName, LicensePlate, Email, Phone string }
}

func main() {
	owner := newClient()
	member := newClient()
	mustPost(owner, "/auth/login", map[string]any{"email": "owner@demo.local", "password": "DemoPass123!"}, 200, "owner login")
	mustPost(member, "/auth/login", map[string]any{"email": "member@demo.local", "password": "DemoPass123!"}, 200, "member login")
	var dashboard struct {
		Communities []struct{ ID, Code string } `json:"communities"`
	}
	must(owner.get("/dashboard/communities", &dashboard))
	if len(dashboard.Communities) == 0 {
		fail("seeded community missing")
	}
	communityID, code := dashboard.Communities[0].ID, dashboard.Communities[0].Code

	newcomer := newClient()
	email := fmt.Sprintf("smoke-%d@example.test", time.Now().UnixNano())
	mustPost(newcomer, "/auth/register", map[string]any{"fullName": "Smoke Test Member", "email": email, "password": "SmokePass123!", "phone": "+8801700999999", "vehicleName": "Mazda 3", "licensePlate": "TEST-9090"}, 201, "register")
	var search struct {
		Communities []struct{ ID string } `json:"communities"`
	}
	must(newcomer.get("/communities/search?q="+url.QueryEscape(code), &search))
	if len(search.Communities) != 1 {
		fail("community search did not return exact seeded community")
	}
	mustPost(newcomer, "/communities/"+communityID+"/join-requests", map[string]any{}, 201, "join request")
	var requests struct {
		Requests []struct {
			ID   string
			User struct{ Email string } `json:"user"`
		} `json:"requests"`
	}
	must(owner.get("/communities/"+communityID+"/join-requests", &requests))
	membershipID := ""
	for _, r := range requests.Requests {
		if r.User.Email == email {
			membershipID = r.ID
		}
	}
	if membershipID == "" {
		fail("new join request missing")
	}
	mustPost(owner, "/communities/"+communityID+"/join-requests/"+membershipID+"/approve", map[string]any{}, 200, "approve")

	var mapResult struct {
		Community struct {
			Cells []cell `json:"cells"`
		} `json:"community"`
	}
	must(newcomer.get("/communities/"+communityID+"/map", &mapResult))
	var occupied, first, second cell
	for _, c := range mapResult.Community.Cells {
		if c.Status == "OCCUPIED" {
			occupied = c
		}
		if c.Status == "AVAILABLE" && first.SlotID == "" {
			first = c
		} else if c.Status == "AVAILABLE" && second.SlotID == "" {
			second = c
		}
	}
	if occupied.Occupant == nil {
		fail("seeded occupied spot missing")
	}
	if occupied.Occupant.Email != "" || occupied.Occupant.Phone != "" || occupied.Occupant.LicensePlate != "" {
		fail("regular member received private occupant fields")
	}
	if first.SlotID == "" || second.SlotID == "" {
		fail("not enough available spots")
	}

	wsURL := "ws://localhost:8080/api/v1/ws/communities/" + communityID
	header := http.Header{}
	u, _ := url.Parse(baseURL)
	cookies := newcomer.http.Jar.Cookies(u)
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		parts = append(parts, c.Name+"="+c.Value)
	}
	header.Set("Cookie", strings.Join(parts, "; "))
	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		if resp != nil {
			fail(fmt.Sprintf("websocket handshake status %d", resp.StatusCode))
		}
		fail("websocket connect: " + err.Error())
	}
	defer ws.Close()
	time.Sleep(100 * time.Millisecond)

	mustPost(newcomer, "/communities/"+communityID+"/slots/"+first.SlotID+"/check-in", map[string]any{}, 201, "check in")
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, message, err := ws.ReadMessage()
	if err != nil {
		fail("websocket event: " + err.Error())
	}
	var event struct{ Type, SlotID string }
	mustJSON(message, &event)
	if event.Type != "SLOT_OCCUPIED" || event.SlotID != first.SlotID {
		fail("unexpected websocket event: " + string(message))
	}
	status, body := newcomer.post("/communities/"+communityID+"/slots/"+second.SlotID+"/check-in", map[string]any{}, nil)
	if status != 409 || errorCode(body) != "USER_ALREADY_PARKED" {
		fail(fmt.Sprintf("one-spot invariant returned %d %s", status, string(body)))
	}
	mustPost(newcomer, "/communities/"+communityID+"/slots/"+first.SlotID+"/check-out", map[string]any{}, 200, "check out")

	var wg sync.WaitGroup
	wg.Add(2)
	statuses := make(chan int, 2)
	go func() {
		defer wg.Done()
		s, _ := owner.post("/communities/"+communityID+"/slots/"+first.SlotID+"/check-in", map[string]any{}, nil)
		statuses <- s
	}()
	go func() {
		defer wg.Done()
		s, _ := newcomer.post("/communities/"+communityID+"/slots/"+first.SlotID+"/check-in", map[string]any{}, nil)
		statuses <- s
	}()
	wg.Wait()
	close(statuses)
	success, conflict := 0, 0
	for s := range statuses {
		if s == 201 {
			success++
		}
		if s == 409 {
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		fail(fmt.Sprintf("concurrent check-in results: success=%d conflict=%d", success, conflict))
	}
	must(owner.get("/communities/"+communityID+"/map", &mapResult))
	var winner string
	for _, c := range mapResult.Community.Cells {
		if c.SlotID == first.SlotID && c.Occupant != nil {
			winner = c.Occupant.UserID
		}
	}
	var me struct {
		User struct{ ID string } `json:"user"`
	}
	must(owner.get("/me", &me))
	cleanup := newcomer
	if winner == me.User.ID {
		cleanup = owner
	}
	mustPost(cleanup, "/communities/"+communityID+"/slots/"+first.SlotID+"/check-out", map[string]any{}, 200, "race cleanup")
	fmt.Println("PASS: auth, discovery, join approval, privacy, WebSocket, occupancy, and concurrency journeys")
}

func newClient() *apiClient {
	jar, _ := cookiejar.New(nil)
	return &apiClient{http: &http.Client{Jar: jar, Timeout: 5 * time.Second}}
}
func (c *apiClient) get(path string, out any) error {
	status, body := c.do(http.MethodGet, path, nil)
	if status != 200 {
		return fmt.Errorf("GET %s: status=%d body=%s", path, status, body)
	}
	return json.Unmarshal(body, out)
}
func (c *apiClient) post(path string, in, out any) (int, []byte) {
	return c.do(http.MethodPost, path, in)
}
func (c *apiClient) do(method, path string, in any) (int, []byte) {
	var body io.Reader
	if in != nil {
		raw, _ := json.Marshal(in)
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, baseURL+path, body)
	if err != nil {
		fail(err.Error())
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		fail(err.Error())
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}
func must(err error) {
	if err != nil {
		fail(err.Error())
	}
}
func mustStatus(status int, body []byte, want int, label string) {
	if status != want {
		fail(fmt.Sprintf("%s: status=%d want=%d body=%s", label, status, want, body))
	}
}
func mustPost(client *apiClient, path string, input any, want int, label string) {
	status, body := client.post(path, input, nil)
	mustStatus(status, body, want, label)
}
func mustJSON(body []byte, out any) {
	if err := json.Unmarshal(body, out); err != nil {
		fail(err.Error())
	}
}
func errorCode(body []byte) string { var e apiError; _ = json.Unmarshal(body, &e); return e.Error.Code }
func fail(message string)          { fmt.Fprintln(os.Stderr, "FAIL:", message); os.Exit(1) }
