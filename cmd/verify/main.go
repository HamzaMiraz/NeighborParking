package main

import (
	"bytes"
	"encoding/json"
	"errors"
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

const (
	apiBase  = "http://localhost:8080/api/v1"
	password = "DemoPass123!"
)

type client struct{ http *http.Client }
type responseError struct {
	Error struct{ Code, Message string } `json:"error"`
}
type user struct{ ID, FullName, Email, Phone, VehicleName, LicensePlate string }
type membership struct {
	ID, UserID, Role, Status string
	User                     user
}
type mapCell struct {
	Row, Col                    int
	Type, Label, SlotID, Status string
	Occupant                    *struct{ UserID, FullName, VehicleName, LicensePlate, Email, Phone string }
}
type community struct {
	ID, Name, Code, Role, Address string
	GridRows, GridCols            int
	LayoutVersion                 uint64
	Cells                         []mapCell
}

func main() {
	step("health and static delivery")
	public := newClient()
	mustCode(public.request("GET", "/health", nil, nil), 200, "health")
	page := rawGet("http://localhost:8080/")
	assert(strings.Contains(page, "NeighborParking") && strings.Contains(page, "/assets/app.js"), "frontend shell missing")

	step("authentication, refresh, profile, and seeded roles")
	owner := newClient()
	admin := newClient()
	member := newClient()
	assertStatus(public.request("GET", "/me", nil, nil), 401, "unauthenticated profile")
	assertError(public.request("POST", "/auth/login", map[string]any{"email": "owner@demo.local", "password": "wrong-password"}, nil), 401, "INVALID_CREDENTIALS", "wrong password")
	login(owner, "owner@demo.local")
	login(admin, "admin@demo.local")
	login(member, "member@demo.local")
	ownerUser := currentUser(owner)
	adminUser := currentUser(admin)
	var refreshed struct{ User user }
	mustCode(owner.request("POST", "/auth/refresh", map[string]any{}, &refreshed), 200, "session refresh")
	assert(refreshed.User.ID == ownerUser.ID, "refresh changed user")
	updated := ownerUser
	updated.Phone = "+8801700000099"
	mustCode(owner.request("PATCH", "/me", map[string]any{"fullName": updated.FullName, "phone": updated.Phone, "vehicleName": updated.VehicleName, "licensePlate": updated.LicensePlate}, nil), 200, "profile update")
	assert(currentUser(owner).Phone == updated.Phone, "profile update was not persisted")
	mustCode(owner.request("PATCH", "/me", map[string]any{"fullName": ownerUser.FullName, "phone": ownerUser.Phone, "vehicleName": ownerUser.VehicleName, "licensePlate": ownerUser.LicensePlate}, nil), 200, "profile restore")

	member2 := ensureAccount("test.member2@demo.local", "Test Member Two", "Mazda CX-5", "TEST-M2-2026")
	outsider := ensureAccount("test.outsider@demo.local", "Test Outsider", "Nissan Note", "TEST-OUT-2026")
	member2User := currentUser(member2)
	outsiderUser := currentUser(outsider)

	lake := dashboardByName(owner, "Lakeview Residency")
	assert(lake.ID != "", "Lakeview seed community missing")
	if existing := memberByEmail(members(owner, lake.ID), outsiderUser.Email); existing.ID != "" {
		mustCode(owner.request("POST", "/communities/"+lake.ID+"/members/"+existing.ID+"/remove", map[string]any{}, nil), 204, "reset outsider membership")
	}
	assertStatus(outsider.request("GET", "/communities/"+lake.ID+"/map", nil, nil), 404, "outsider map concealment")

	step("community discovery, rejection, re-request, and approval")
	var search struct{ Communities []map[string]any }
	mustCode(outsider.request("GET", "/communities/search?q="+url.QueryEscape(lake.Code), nil, &search), 200, "community search")
	assert(len(search.Communities) == 1, "exact community-code search failed")
	assert(search.Communities[0]["address"] == nil && search.Communities[0]["availableSpots"] == nil, "search leaked private community data")
	member2Membership := ensureApproved(owner, member2, lake.ID, member2User.Email)
	mustCode(outsider.request("POST", "/communities/"+lake.ID+"/join-requests", map[string]any{}, nil), 201, "outsider join request")
	assertError(outsider.request("POST", "/communities/"+lake.ID+"/join-requests", map[string]any{}, nil), 409, "REQUEST_ALREADY_PENDING", "duplicate pending request")
	request := pendingByEmail(owner, lake.ID, outsiderUser.Email)
	mustCode(owner.request("POST", "/communities/"+lake.ID+"/join-requests/"+request.ID+"/reject", map[string]any{}, nil), 200, "reject join")
	mustCode(outsider.request("POST", "/communities/"+lake.ID+"/join-requests", map[string]any{}, nil), 201, "re-request after rejection")
	request = pendingByEmail(owner, lake.ID, outsiderUser.Email)
	mustCode(owner.request("POST", "/communities/"+lake.ID+"/join-requests/"+request.ID+"/approve", map[string]any{}, nil), 200, "approve re-request")
	assertError(outsider.request("POST", "/communities/"+lake.ID+"/join-requests", map[string]any{}, nil), 409, "ALREADY_MEMBER", "approved duplicate request")
	outsiderLakeMembership := memberByEmail(members(owner, lake.ID), outsiderUser.Email)
	assertError(member2.request("PATCH", "/communities/"+lake.ID+"/members/"+outsiderLakeMembership.ID+"/role", map[string]any{"role": "ADMIN"}, nil), 403, "ADMIN_REQUIRED", "member role escalation")
	assertError(member2.request("GET", "/communities/"+lake.ID+"/join-requests", nil, nil), 403, "ADMIN_REQUIRED", "member request-list access")

	step("member privacy and administrator detail visibility")
	regularMap := getMap(member2, lake.ID)
	occupied := findOccupied(regularMap)
	assert(occupied.Occupant != nil, "seeded occupied spot missing")
	assert(occupied.Occupant.Email == "" && occupied.Occupant.Phone == "" && occupied.Occupant.LicensePlate == "", "regular member received private occupant fields")
	adminMap := getMap(owner, lake.ID)
	occupied = findOccupied(adminMap)
	assert(occupied.Occupant != nil && occupied.Occupant.Email != "" && occupied.Occupant.LicensePlate != "", "admin did not receive permitted occupant details")
	regularMembers := members(member2, lake.ID)
	ownerMembers := members(owner, lake.ID)
	seededMemberUser := currentUser(member)
	regularSeeded := memberByUserID(regularMembers, seededMemberUser.ID)
	assert(regularSeeded.ID != "" && regularSeeded.User.Email == "", "member directory leaked email or omitted member")
	assert(memberByEmail(ownerMembers, "member@demo.local").User.Email != "", "admin directory omitted email")

	step("promotion, demotion, and atomic ownership transfer")
	mustCode(owner.request("PATCH", "/communities/"+lake.ID+"/members/"+member2Membership.ID+"/role", map[string]any{"role": "ADMIN"}, nil), 200, "promote member")
	assert(memberByEmail(members(owner, lake.ID), member2User.Email).Role == "ADMIN", "promotion not persisted")
	mustCode(owner.request("PATCH", "/communities/"+lake.ID+"/members/"+member2Membership.ID+"/role", map[string]any{"role": "MEMBER"}, nil), 200, "demote admin")
	mustCode(owner.request("POST", "/communities/"+lake.ID+"/ownership/transfer", map[string]any{"userId": adminUser.ID}, nil), 200, "transfer ownership to admin")
	assert(dashboardByName(admin, "Lakeview Residency").Role == "OWNER", "new owner role missing")
	mustCode(admin.request("POST", "/communities/"+lake.ID+"/ownership/transfer", map[string]any{"userId": ownerUser.ID}, nil), 200, "transfer ownership back")
	assert(dashboardByName(owner, "Lakeview Residency").Role == "OWNER", "original ownership not restored")

	step("multi-community creation and tenant isolation")
	var created struct{ Community community }
	mustCode(member2.request("POST", "/communities", map[string]any{"name": "QA Verification Court " + fmt.Sprint(time.Now().Unix()), "description": "Automated full-feature verification community", "address": "Private QA address", "gridRows": 3, "gridCols": 4}, &created), 201, "create QA community")
	qa := created.Community
	assert(qa.Role == "OWNER", "community creator did not become owner")
	assertStatus(owner.request("GET", "/communities/"+qa.ID+"/map", nil, nil), 404, "cross-community concealment")
	ownerQAMembership := ensureApproved(member2, owner, qa.ID, ownerUser.Email)
	mustCode(member2.request("PATCH", "/communities/"+qa.ID+"/members/"+ownerQAMembership.ID+"/role", map[string]any{"role": "ADMIN"}, nil), 200, "QA admin promotion")
	outsiderQAMembership := ensureApproved(member2, outsider, qa.ID, outsiderUser.Email)
	assert(len(dashboard(member2)) >= 2, "multi-community dashboard missing community")
	assertError(outsider.request("PUT", "/communities/"+qa.ID+"/layout", map[string]any{"rows": 2, "cols": 2, "layoutVersion": qa.LayoutVersion, "cells": []any{}}, nil), 403, "ADMIN_REQUIRED", "member layout edit")

	step("layout design, stale versions, and occupied-cell protection")
	layout := []map[string]any{
		{"row": 0, "col": 0, "type": "ENTRY_GATE", "label": "Entry"}, {"row": 0, "col": 1, "type": "ROAD"}, {"row": 0, "col": 2, "type": "ROAD"}, {"row": 0, "col": 3, "type": "EXIT_GATE", "label": "Exit"},
		{"row": 1, "col": 0, "type": "PARKING", "label": "Q-01"}, {"row": 1, "col": 1, "type": "PARKING", "label": "Q-02"}, {"row": 1, "col": 2, "type": "PILLAR"}, {"row": 1, "col": 3, "type": "PARKING", "label": "Q-03"},
		{"row": 2, "col": 0, "type": "WALL"}, {"row": 2, "col": 1, "type": "ROAD"}, {"row": 2, "col": 2, "type": "NO_PARKING", "label": "Keep clear"}, {"row": 2, "col": 3, "type": "WALL"},
	}
	mustCode(member2.request("PUT", "/communities/"+qa.ID+"/layout", map[string]any{"rows": 3, "cols": 4, "layoutVersion": qa.LayoutVersion, "cells": layout}, nil), 200, "save layout")
	assertError(member2.request("PUT", "/communities/"+qa.ID+"/layout", map[string]any{"rows": 3, "cols": 4, "layoutVersion": qa.LayoutVersion, "cells": layout}, nil), 409, "LAYOUT_VERSION_CONFLICT", "stale layout")
	qaMap := getMap(member2, qa.ID)
	q1 := slotByLabel(qaMap, "Q-01")
	q2 := slotByLabel(qaMap, "Q-02")
	q3 := slotByLabel(qaMap, "Q-03")
	mustCode(member2.request("POST", slotPath(qa.ID, q1.SlotID, "check-in"), map[string]any{}, nil), 201, "owner self check-in")
	badLayout := cloneLayout(layout)
	for _, c := range badLayout {
		if c["label"] == "Q-01" {
			c["label"] = "RENAMED"
		}
	}
	current := getMap(member2, qa.ID)
	assertError(member2.request("PUT", "/communities/"+qa.ID+"/layout", map[string]any{"rows": 3, "cols": 4, "layoutVersion": current.LayoutVersion, "cells": badLayout}, nil), 409, "OCCUPIED_SLOT_IMMUTABLE", "occupied layout mutation")
	assertError(outsider.request("POST", slotPath(qa.ID, q1.SlotID, "check-out"), map[string]any{}, nil), 403, "NOT_SLOT_OCCUPANT", "other-member checkout")
	mustCode(owner.request("POST", slotPath(qa.ID, q1.SlotID, "force-check-out"), map[string]any{}, nil), 200, "admin force checkout")

	step("force actions and one-active-space invariant")
	mustCode(member2.request("POST", slotPath(qa.ID, q1.SlotID, "force-check-in"), map[string]any{"userId": ownerUser.ID}, nil), 201, "owner force check-in")
	mustCode(owner.request("POST", slotPath(qa.ID, q1.SlotID, "check-out"), map[string]any{}, nil), 200, "assigned user checkout")
	mustCode(member2.request("POST", slotPath(qa.ID, q1.SlotID, "check-in"), map[string]any{}, nil), 201, "self check-in")
	assertError(member2.request("POST", slotPath(qa.ID, q2.SlotID, "check-in"), map[string]any{}, nil), 409, "USER_ALREADY_PARKED", "second active space")
	mustCode(member2.request("POST", slotPath(qa.ID, q1.SlotID, "check-out"), map[string]any{}, nil), 200, "self checkout")

	step("same-millisecond booking race")
	statuses := make(chan result, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		statuses <- member2.request("POST", slotPath(qa.ID, q1.SlotID, "check-in"), map[string]any{}, nil)
	}()
	go func() {
		defer wg.Done()
		statuses <- owner.request("POST", slotPath(qa.ID, q1.SlotID, "check-in"), map[string]any{}, nil)
	}()
	wg.Wait()
	close(statuses)
	success, conflict := 0, 0
	for r := range statuses {
		if r.status == 201 {
			success++
		}
		if r.status == 409 && errorCode(r.body) == "SLOT_ALREADY_OCCUPIED" {
			conflict++
		}
	}
	assert(success == 1 && conflict == 1, fmt.Sprintf("booking race result success=%d conflict=%d", success, conflict))
	winnerMap := getMap(member2, qa.ID)
	winner := slotByLabel(winnerMap, "Q-01").Occupant.UserID
	if winner == member2User.ID {
		mustCode(member2.request("POST", slotPath(qa.ID, q1.SlotID, "check-out"), map[string]any{}, nil), 200, "race cleanup")
	} else {
		mustCode(owner.request("POST", slotPath(qa.ID, q1.SlotID, "check-out"), map[string]any{}, nil), 200, "race cleanup")
	}

	step("archived-slot history and safe name/position reuse")
	mustCode(member2.request("POST", slotPath(qa.ID, q2.SlotID, "check-in"), map[string]any{}, nil), 201, "historical slot check-in")
	mustCode(member2.request("POST", slotPath(qa.ID, q2.SlotID, "check-out"), map[string]any{}, nil), 200, "historical slot check-out")
	current = getMap(member2, qa.ID)
	withoutQ2 := filterLabel(layout, "Q-02")
	mustCode(member2.request("PUT", "/communities/"+qa.ID+"/layout", map[string]any{"rows": 3, "cols": 4, "layoutVersion": current.LayoutVersion, "cells": withoutQ2}, nil), 200, "remove available historical slot")
	current = getMap(member2, qa.ID)
	mustCode(member2.request("PUT", "/communities/"+qa.ID+"/layout", map[string]any{"rows": 3, "cols": 4, "layoutVersion": current.LayoutVersion, "cells": layout}, nil), 200, "reuse archived slot name and position")
	qaMap = getMap(member2, qa.ID)
	q1 = slotByLabel(qaMap, "Q-01")
	q3 = slotByLabel(qaMap, "Q-03")

	step("community-scoped WebSockets")
	qaSocket := openSocket(outsider, qa.ID)
	lakeSocket := openSocket(outsider, lake.ID)
	defer qaSocket.Close()
	defer lakeSocket.Close()
	time.Sleep(100 * time.Millisecond)
	mustCode(member2.request("POST", slotPath(qa.ID, q3.SlotID, "check-in"), map[string]any{}, nil), 201, "websocket trigger check-in")
	expectEvent(qaSocket, "SLOT_OCCUPIED", q3.SlotID)
	_ = lakeSocket.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	_, _, err := lakeSocket.ReadMessage()
	assert(err != nil, "QA event leaked into Lakeview socket")
	mustCode(member2.request("POST", slotPath(qa.ID, q3.SlotID, "check-out"), map[string]any{}, nil), 200, "websocket cleanup")

	step("remove occupied member, force release, and immediate access revocation")
	mustCode(outsider.request("POST", slotPath(qa.ID, q1.SlotID, "check-in"), map[string]any{}, nil), 201, "removed member precondition")
	qaSocket2 := openSocket(outsider, qa.ID)
	time.Sleep(100 * time.Millisecond)
	mustCode(member2.request("POST", "/communities/"+qa.ID+"/members/"+outsiderQAMembership.ID+"/remove", map[string]any{}, nil), 204, "remove occupied member")
	_ = qaSocket2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = qaSocket2.ReadMessage()
	assert(err != nil, "removed member WebSocket remained open")
	_ = qaSocket2.Close()
	assertStatus(outsider.request("GET", "/communities/"+qa.ID+"/map", nil, nil), 404, "removed member access")
	assert(slotByLabel(getMap(member2, qa.ID), "Q-01").Status == "AVAILABLE", "member removal did not release slot")
	outsiderQAMembership = ensureApproved(member2, outsider, qa.ID, outsiderUser.Email)

	step("ban, unban, rejoin, leave, and owner guard")
	mustCode(member2.request("POST", "/communities/"+qa.ID+"/members/"+outsiderQAMembership.ID+"/ban", map[string]any{}, nil), 204, "ban member")
	assertStatus(outsider.request("GET", "/communities/"+qa.ID+"/map", nil, nil), 404, "banned access")
	assertError(outsider.request("POST", "/communities/"+qa.ID+"/join-requests", map[string]any{}, nil), 403, "MEMBERSHIP_BANNED", "banned rejoin")
	mustCode(member2.request("POST", "/communities/"+qa.ID+"/members/"+outsiderQAMembership.ID+"/unban", map[string]any{}, nil), 204, "unban member")
	outsiderQAMembership = ensureApproved(member2, outsider, qa.ID, outsiderUser.Email)
	qaMap = getMap(member2, qa.ID)
	q1 = slotByLabel(qaMap, "Q-01")
	mustCode(outsider.request("POST", slotPath(qa.ID, q1.SlotID, "check-in"), map[string]any{}, nil), 201, "leave-while-parked precondition")
	assertError(outsider.request("POST", "/communities/"+qa.ID+"/leave", map[string]any{}, nil), 409, "ACTIVE_OCCUPANCY_EXISTS", "leave while parked")
	mustCode(outsider.request("POST", slotPath(qa.ID, q1.SlotID, "check-out"), map[string]any{}, nil), 200, "leave-while-parked cleanup")
	mustCode(outsider.request("POST", "/communities/"+qa.ID+"/leave", map[string]any{}, nil), 204, "leave community")
	assertStatus(outsider.request("GET", "/communities/"+qa.ID+"/map", nil, nil), 404, "left member access")
	assertError(member2.request("POST", "/communities/"+qa.ID+"/leave", map[string]any{}, nil), 409, "LAST_OWNER_CANNOT_LEAVE", "owner leave guard")

	step("logout invalidation")
	mustCode(outsider.request("POST", "/auth/logout", map[string]any{}, nil), 204, "logout")
	assertStatus(outsider.request("GET", "/me", nil, nil), 401, "logged-out session")

	fmt.Println("PASS: all NeighborParking API, transaction, privacy, role, layout, occupancy, and WebSocket feature checks")
}

type result struct {
	status int
	body   []byte
}

func step(s string) { fmt.Println("CHECK:", s) }
func newClient() *client {
	jar, _ := cookiejar.New(nil)
	return &client{http: &http.Client{Jar: jar, Timeout: 6 * time.Second}}
}
func ensureAccount(email, name, vehicle, plate string) *client {
	c := newClient()
	r := c.request("POST", "/auth/login", map[string]any{"email": email, "password": password}, nil)
	if r.status == 200 {
		return c
	}
	mustCode(c.request("POST", "/auth/register", map[string]any{"fullName": name, "email": email, "password": password, "phone": "+8801700888800", "vehicleName": vehicle, "licensePlate": plate}, nil), 201, "register "+email)
	return c
}
func login(c *client, email string) {
	mustCode(c.request("POST", "/auth/login", map[string]any{"email": email, "password": password}, nil), 200, "login "+email)
}
func currentUser(c *client) user {
	var out struct{ User user }
	mustCode(c.request("GET", "/me", nil, &out), 200, "current user")
	return out.User
}
func dashboard(c *client) []community {
	var out struct{ Communities []community }
	mustCode(c.request("GET", "/dashboard/communities", nil, &out), 200, "dashboard")
	return out.Communities
}
func dashboardByName(c *client, name string) community {
	for _, x := range dashboard(c) {
		if x.Name == name {
			return x
		}
	}
	return community{}
}
func getMap(c *client, id string) community {
	var out struct{ Community community }
	mustCode(c.request("GET", "/communities/"+id+"/map", nil, &out), 200, "map")
	return out.Community
}
func members(c *client, id string) []membership {
	var out struct{ Members []membership }
	mustCode(c.request("GET", "/communities/"+id+"/members", nil, &out), 200, "members")
	return out.Members
}
func memberByEmail(items []membership, email string) membership {
	for _, m := range items {
		if m.User.Email == email || m.User.FullName == "Test Member Two" && email == "test.member2@demo.local" {
			return m
		}
	}
	return membership{}
}
func memberByUserID(items []membership, id string) membership {
	for _, m := range items {
		if m.UserID == id {
			return m
		}
	}
	return membership{}
}
func pendingByEmail(admin *client, cid, email string) membership {
	var out struct{ Requests []membership }
	mustCode(admin.request("GET", "/communities/"+cid+"/join-requests", nil, &out), 200, "pending requests")
	for _, m := range out.Requests {
		if m.User.Email == email {
			return m
		}
	}
	fail("pending request missing for " + email)
	return membership{}
}
func ensureApproved(admin, userClient *client, cid, email string) membership {
	for _, m := range members(admin, cid) {
		if m.User.Email == email {
			return m
		}
	}
	r := userClient.request("POST", "/communities/"+cid+"/join-requests", map[string]any{}, nil)
	if r.status != 201 {
		fail(fmt.Sprintf("join %s: status=%d body=%s", email, r.status, r.body))
	}
	request := pendingByEmail(admin, cid, email)
	mustCode(admin.request("POST", "/communities/"+cid+"/join-requests/"+request.ID+"/approve", map[string]any{}, nil), 200, "approve "+email)
	return memberByEmail(members(admin, cid), email)
}
func findOccupied(c community) mapCell {
	for _, x := range c.Cells {
		if x.Status == "OCCUPIED" {
			return x
		}
	}
	return mapCell{}
}
func slotByLabel(c community, label string) mapCell {
	for _, x := range c.Cells {
		if x.Label == label {
			return x
		}
	}
	fail("slot missing: " + label)
	return mapCell{}
}
func slotPath(cid, sid, action string) string {
	return "/communities/" + cid + "/slots/" + sid + "/" + action
}
func cloneLayout(in []map[string]any) []map[string]any {
	raw, _ := json.Marshal(in)
	var out []map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}
func filterLabel(in []map[string]any, label string) []map[string]any {
	out := []map[string]any{}
	for _, x := range cloneLayout(in) {
		if x["label"] != label {
			out = append(out, x)
		}
	}
	return out
}
func openSocket(c *client, cid string) *websocket.Conn {
	u, _ := url.Parse(apiBase)
	parts := []string{}
	for _, cookie := range c.http.Jar.Cookies(u) {
		parts = append(parts, cookie.Name+"="+cookie.Value)
	}
	h := http.Header{"Cookie": []string{strings.Join(parts, "; ")}}
	ws, resp, err := websocket.DefaultDialer.Dial("ws://localhost:8080/api/v1/ws/communities/"+cid, h)
	if err != nil {
		if resp != nil {
			fail(fmt.Sprintf("websocket status %d", resp.StatusCode))
		}
		fail(err.Error())
	}
	return ws
}
func expectEvent(ws *websocket.Conn, eventType, slotID string) {
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, body, err := ws.ReadMessage()
	if err != nil {
		fail("websocket read: " + err.Error())
	}
	var e struct{ Type, SlotID string }
	mustJSON(body, &e)
	assert(e.Type == eventType && e.SlotID == slotID, "unexpected WebSocket event "+string(body))
}
func (c *client) request(method, path string, input, out any) result {
	var body io.Reader
	if input != nil {
		raw, _ := json.Marshal(input)
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, apiBase+path, body)
	if err != nil {
		fail(err.Error())
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		fail(err.Error())
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && len(raw) > 0 {
		mustJSON(raw, out)
	}
	return result{resp.StatusCode, raw}
}
func rawGet(target string) string {
	resp, err := http.Get(target)
	if err != nil {
		fail(err.Error())
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	assert(resp.StatusCode == 200, "GET "+target)
	return string(raw)
}
func mustCode(r result, want int, label string) {
	if r.status != want {
		fail(fmt.Sprintf("%s: status=%d want=%d body=%s", label, r.status, want, r.body))
	}
}
func assertStatus(r result, want int, label string) {
	if r.status != want {
		fail(fmt.Sprintf("%s: status=%d want=%d", label, r.status, want))
	}
}
func assertError(r result, want int, code, label string) {
	if r.status != want || errorCode(r.body) != code {
		fail(fmt.Sprintf("%s: status=%d code=%s body=%s", label, r.status, errorCode(r.body), r.body))
	}
}
func errorCode(body []byte) string {
	var e responseError
	_ = json.Unmarshal(body, &e)
	return e.Error.Code
}
func mustJSON(body []byte, out any) {
	if err := json.Unmarshal(body, out); err != nil {
		fail(err.Error() + ": " + string(body))
	}
}
func assert(ok bool, message string) {
	if !ok {
		fail(message)
	}
}
func fail(message string) { fmt.Fprintln(os.Stderr, "FAIL:", message); os.Exit(1) }

var _ = errors.Is
