package main

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/http/response"
)

type apiError struct {
	code int
	msg  string
}

func (e apiError) Error() string   { return e.msg }
func (e apiError) StatusCode() int { return e.code }

func badRequest(format string, a ...any) error {
	return apiError{http.StatusBadRequest, fmt.Sprintf(format, a...)}
}

type server struct {
	fb    *firebaseClient // nil when Firebase is not configured
	fbErr error
}

// ---------- locations ----------

type locationPoint struct {
	Latitude  float64  `json:"latitude"`
	Longitude float64  `json:"longitude"`
	Accuracy  *float64 `json:"accuracy"`
	Altitude  *float64 `json:"altitude"`
	Speed     *float64 `json:"speed"`
	Heading   *float64 `json:"heading"`
	Timestamp int64    `json:"timestamp"`
}

// locationRequest accepts both the mobile app's batch payload ("locations": [...])
// and a single flat location (latitude/longitude/timestamp at the top level).
type locationRequest struct {
	locationPoint
	EmployeeID string          `json:"employeeId"`
	UID        string          `json:"uid"`
	SessionID  string          `json:"sessionId"`
	Platform   string          `json:"platform"`
	Type       string          `json:"type"`
	SentAt     string          `json:"sentAt"`
	Locations  []locationPoint `json:"locations"`
}

func (s *server) postLocations(c *gofr.Context) (any, error) {
	var req locationRequest
	if err := c.Bind(&req); err != nil {
		return nil, badRequest("invalid JSON body: %v", err)
	}

	employeeID := strings.TrimSpace(req.EmployeeID)
	if employeeID == "" {
		return nil, badRequest("employeeId is required")
	}

	points := req.Locations
	if len(points) == 0 && (req.Latitude != 0 || req.Longitude != 0) {
		points = []locationPoint{req.locationPoint}
	}
	if len(points) == 0 {
		return nil, badRequest("provide either a single location (latitude, longitude) or a non-empty locations array")
	}
	if len(points) > 5000 {
		return nil, badRequest("too many locations in one request (max 5000)")
	}

	for i, p := range points {
		if p.Latitude < -90 || p.Latitude > 90 || p.Longitude < -180 || p.Longitude > 180 {
			return nil, badRequest("locations[%d]: latitude/longitude out of range", i)
		}
	}

	tx, err := c.SQL.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UnixMilli()
	inserted := 0
	for _, p := range points {
		res, err := tx.ExecContext(c,
			`INSERT OR IGNORE INTO locations
			 (employee_id, session_id, platform, latitude, longitude, accuracy, altitude, speed, heading, recorded_at, sent_at, received_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			employeeID, req.SessionID, req.Platform, p.Latitude, p.Longitude,
			p.Accuracy, p.Altitude, p.Speed, p.Heading, normalizeTimestamp(p.Timestamp), req.SentAt, now)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
		}
	}

	if uid := strings.TrimSpace(req.UID); uid != "" {
		// Learn employeeId <-> Firebase uid so notifications can target by employeeId.
		if _, err := tx.ExecContext(c,
			`INSERT OR REPLACE INTO employee_uids (employee_id, uid, updated_at) VALUES (?, ?, ?)`,
			employeeID, uid, now); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return map[string]any{
		"employeeId": employeeID,
		"received":   len(points),
		"stored":     inserted,
		"duplicates": len(points) - inserted,
	}, nil
}

type locationRow struct {
	ID            int64    `json:"id"`
	Latitude      float64  `json:"latitude"`
	Longitude     float64  `json:"longitude"`
	Accuracy      *float64 `json:"accuracy"`
	Altitude      *float64 `json:"altitude"`
	Speed         *float64 `json:"speed"`
	Heading       *float64 `json:"heading"`
	Platform      string   `json:"platform"`
	RecordedAt    int64    `json:"recordedAt"`
	RecordedAtISO string   `json:"recordedAtISO"`
}

func (s *server) getLocations(c *gofr.Context) (any, error) {
	userID := c.PathParam("employeeId")

	limit := 100
	if v := c.Param("limit"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &limit); err != nil || limit < 1 {
			return nil, badRequest("limit must be a positive integer")
		}
		if limit > 1000 {
			limit = 1000
		}
	}

	offset := 0
	if v := c.Param("offset"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &offset); err != nil || offset < 0 {
			return nil, badRequest("offset must be a non-negative integer")
		}
	}

	from, err := parseTimeParam(c.Param("from"))
	if err != nil {
		return nil, badRequest("from must be epoch ms or RFC3339")
	}
	to, err := parseTimeParam(c.Param("to"))
	if err != nil {
		return nil, badRequest("to must be epoch ms or RFC3339")
	}
	if to == 0 {
		to = time.Now().Add(24 * time.Hour).UnixMilli()
	}

	order := "DESC"
	if strings.EqualFold(c.Param("order"), "asc") {
		order = "ASC"
	}

	var total int
	if err := c.SQL.QueryRowContext(c,
		`SELECT COUNT(*) FROM locations WHERE employee_id = ? AND recorded_at BETWEEN ? AND ?`,
		userID, from, to).Scan(&total); err != nil {
		return nil, err
	}

	rows, err := c.SQL.QueryContext(c,
		`SELECT id, latitude, longitude, accuracy, altitude, speed, heading, COALESCE(platform, ''), recorded_at
		 FROM locations WHERE employee_id = ? AND recorded_at BETWEEN ? AND ?
		 ORDER BY recorded_at `+order+` LIMIT ? OFFSET ?`,
		userID, from, to, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []locationRow{}
	for rows.Next() {
		var r locationRow
		if err := rows.Scan(&r.ID, &r.Latitude, &r.Longitude, &r.Accuracy, &r.Altitude, &r.Speed, &r.Heading, &r.Platform, &r.RecordedAt); err != nil {
			return nil, err
		}
		r.RecordedAtISO = time.UnixMilli(r.RecordedAt).UTC().Format(time.RFC3339)
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return map[string]any{
		"employeeId": userID,
		"total":      total,
		"count":      len(items),
		"limit":      limit,
		"offset":     offset,
		"locations":  items,
	}, nil
}

func (s *server) getLatestLocation(c *gofr.Context) (any, error) {
	userID := c.PathParam("employeeId")

	var r locationRow
	err := c.SQL.QueryRowContext(c,
		`SELECT id, latitude, longitude, accuracy, altitude, speed, heading, COALESCE(platform, ''), recorded_at
		 FROM locations WHERE employee_id = ? ORDER BY recorded_at DESC LIMIT 1`, userID).
		Scan(&r.ID, &r.Latitude, &r.Longitude, &r.Accuracy, &r.Altitude, &r.Speed, &r.Heading, &r.Platform, &r.RecordedAt)
	if err != nil {
		return nil, apiError{http.StatusNotFound, fmt.Sprintf("no locations found for employeeId %q", userID)}
	}
	r.RecordedAtISO = time.UnixMilli(r.RecordedAt).UTC().Format(time.RFC3339)

	return map[string]any{"employeeId": userID, "location": r}, nil
}

// ---------- employees / registered users (Firebase) ----------

// firestoreTokens reads FCM tokens the app stored at users/{uid}/fcmTokens/*.
func (s *server) firestoreTokens(c *gofr.Context) ([]tokenRef, error) {
	if s.fb == nil {
		return nil, apiError{http.StatusServiceUnavailable, s.fbErr.Error()}
	}

	toks, err := s.fb.listTokens(c)
	if err != nil {
		return nil, apiError{http.StatusBadGateway, err.Error()}
	}

	return toks, nil
}

// uidMaps returns employeeId -> uid and uid -> employeeId (learned from POST /locations).
func uidMaps(c *gofr.Context) (map[string]string, map[string]string, error) {
	rows, err := c.SQL.QueryContext(c, `SELECT employee_id, uid FROM employee_uids`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	empToUID, uidToEmp := map[string]string{}, map[string]string{}
	for rows.Next() {
		var emp, uid string
		if err := rows.Scan(&emp, &uid); err != nil {
			return nil, nil, err
		}
		empToUID[emp], uidToEmp[uid] = uid, emp
	}

	return empToUID, uidToEmp, rows.Err()
}

func (s *server) getEmployeesCount(c *gofr.Context) (any, error) {
	var locEmployees, locTotal, mapped int
	if err := c.SQL.QueryRowContext(c, `SELECT COUNT(DISTINCT employee_id), COUNT(*) FROM locations`).Scan(&locEmployees, &locTotal); err != nil {
		return nil, err
	}
	if err := c.SQL.QueryRowContext(c, `SELECT COUNT(*) FROM employee_uids`).Scan(&mapped); err != nil {
		return nil, err
	}

	res := map[string]any{
		"employeesWithLocations": locEmployees,
		"totalLocations":         locTotal,
		"mappedEmployees":        mapped,
		"firebaseEnabled":        s.fb != nil,
	}

	toks, err := s.firestoreTokens(c)
	if err != nil {
		res["warning"] = err.Error()
		return res, nil
	}

	uids := map[string]bool{}
	for _, t := range toks {
		uids[t.UID] = true
	}
	res["registeredUsers"] = len(uids)
	res["deviceTokens"] = len(toks)

	return res, nil
}

func (s *server) getEmployees(c *gofr.Context) (any, error) {
	type employee struct {
		EmployeeID    *string  `json:"employeeId"`
		UID           string   `json:"uid,omitempty"`
		DeviceTokens  int      `json:"deviceTokens"`
		LocationCount int      `json:"locationCount"`
		LastSeenISO   string   `json:"lastSeenISO,omitempty"`
		LastLatitude  *float64 `json:"lastLatitude,omitempty"`
		LastLongitude *float64 `json:"lastLongitude,omitempty"`
	}

	empToUID, _, err := uidMaps(c)
	if err != nil {
		return nil, err
	}

	res := map[string]any{}
	tokenCount := map[string]int{}
	if toks, err := s.firestoreTokens(c); err != nil {
		res["warning"] = err.Error()
	} else {
		for _, t := range toks {
			tokenCount[t.UID]++
		}
	}

	byEmp := map[string]*employee{}
	get := func(id string) *employee {
		if e, ok := byEmp[id]; ok {
			return e
		}
		idCopy := id
		e := &employee{EmployeeID: &idCopy, UID: empToUID[id], DeviceTokens: tokenCount[empToUID[id]]}
		byEmp[id] = e

		return e
	}

	for id := range empToUID {
		get(id)
	}

	rows, err := c.SQL.QueryContext(c,
		`SELECT l.employee_id, c.cnt, l.recorded_at, l.latitude, l.longitude
		 FROM locations l
		 JOIN (SELECT employee_id, COUNT(*) AS cnt, MAX(recorded_at) AS m FROM locations GROUP BY employee_id) c
		   ON c.employee_id = l.employee_id AND c.m = l.recorded_at
		 GROUP BY l.employee_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var cnt int
		var at int64
		var lat, lon float64
		if err := rows.Scan(&id, &cnt, &at, &lat, &lon); err != nil {
			return nil, err
		}
		e := get(id)
		e.LocationCount, e.LastLatitude, e.LastLongitude = cnt, &lat, &lon
		e.LastSeenISO = time.UnixMilli(at).UTC().Format(time.RFC3339)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(byEmp))
	for id := range byEmp {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	list := make([]*employee, 0, len(ids))
	known := map[string]bool{}
	for _, id := range ids {
		list = append(list, byEmp[id])
		known[byEmp[id].UID] = true
	}

	// Registered Firebase users that have not yet posted a location (employeeId unknown).
	var orphans []string
	for uid := range tokenCount {
		if !known[uid] {
			orphans = append(orphans, uid)
		}
	}
	sort.Strings(orphans)
	for _, uid := range orphans {
		list = append(list, &employee{UID: uid, DeviceTokens: tokenCount[uid]})
	}

	res["count"] = len(list)
	res["employees"] = list

	return res, nil
}

// ---------- notifications ----------

type sendRequest struct {
	EmployeeID  string            `json:"employeeId"`
	EmployeeIDs []string          `json:"employeeIds"`
	UID         string            `json:"uid"`
	UIDs        []string          `json:"uids"`
	All         bool              `json:"all"`
	Title       string            `json:"title"`
	Body        string            `json:"body"`
	ImageURL    string            `json:"imageUrl"`
	Data        map[string]string `json:"data"`
}

type sendResult struct {
	EmployeeID string `json:"employeeId,omitempty"`
	UID        string `json:"uid"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

func (s *server) postNotification(c *gofr.Context) (any, error) {
	if s.fb == nil {
		return nil, apiError{http.StatusServiceUnavailable, s.fbErr.Error()}
	}

	var req sendRequest
	if err := c.Bind(&req); err != nil {
		return nil, badRequest("invalid JSON body: %v", err)
	}
	if strings.TrimSpace(req.Title) == "" && strings.TrimSpace(req.Body) == "" {
		return nil, badRequest("title or body is required")
	}

	empIDs := req.EmployeeIDs
	if req.EmployeeID != "" {
		empIDs = append(empIDs, req.EmployeeID)
	}
	uidTargets := req.UIDs
	if req.UID != "" {
		uidTargets = append(uidTargets, req.UID)
	}
	if !req.All && len(empIDs) == 0 && len(uidTargets) == 0 {
		return nil, badRequest(`specify "employeeId", "employeeIds", "uid", "uids" or "all": true`)
	}

	empToUID, uidToEmp, err := uidMaps(c)
	if err != nil {
		return nil, err
	}

	wanted := map[string]bool{}
	var unmapped []string
	for _, id := range empIDs {
		if uid, ok := empToUID[id]; ok {
			wanted[uid] = true
		} else {
			unmapped = append(unmapped, id)
		}
	}
	for _, uid := range uidTargets {
		wanted[uid] = true
	}

	all, err := s.firestoreTokens(c)
	if err != nil {
		return nil, err
	}

	var jobs []tokenRef
	hasTokens := map[string]bool{}
	for _, t := range all {
		hasTokens[t.UID] = true
		if req.All || wanted[t.UID] {
			jobs = append(jobs, t)
		}
	}

	var noTokens []string
	for uid := range wanted {
		if !hasTokens[uid] {
			noTokens = append(noTokens, uid)
		}
	}
	sort.Strings(noTokens)

	if len(jobs) == 0 {
		return nil, apiError{http.StatusNotFound, fmt.Sprintf(
			"no registered device tokens found (employeeIds without a known uid: %v; uids without tokens: %v)", unmapped, noTokens)}
	}

	n := notification{Title: req.Title, Body: req.Body, ImageURL: req.ImageURL, Data: req.Data}
	results := make([]sendResult, len(jobs))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)
	for i, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, j tokenRef) {
			defer wg.Done()
			defer func() { <-sem }()

			dead, err := s.fb.send(c, j.Token, n)
			results[i] = sendResult{EmployeeID: uidToEmp[j.UID], UID: j.UID, Success: err == nil}
			if err != nil {
				results[i].Error = err.Error()
			}
			if dead {
				_ = s.fb.deleteDoc(c, j.DocName)
			}
		}(i, j)
	}
	wg.Wait()

	sent := 0
	for _, r := range results {
		if r.Success {
			sent++
		}
	}

	return map[string]any{
		"targetedTokens":        len(jobs),
		"sent":                  sent,
		"failed":                len(jobs) - sent,
		"employeeIdsWithoutUid": unmapped,
		"uidsWithoutTokens":     noTokens,
		"results":               results,
	}, nil
}

// ---------- deletes (development / testing) ----------

// deletedResponse carries the count in a header because gofr sends DELETE as 204 No Content.
func deletedResponse(n int64) response.Response {
	return response.Response{
		Data:    map[string]any{"deleted": n},
		Headers: map[string]string{"X-Deleted-Count": strconv.FormatInt(n, 10)},
	}
}

func (s *server) deleteEmployeeLocations(c *gofr.Context) (any, error) {
	res, err := c.SQL.ExecContext(c, `DELETE FROM locations WHERE employee_id = ?`, c.PathParam("employeeId"))
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()

	return deletedResponse(n), nil
}

func (s *server) deleteAllLocations(c *gofr.Context) (any, error) {
	if c.Param("confirm") != "true" {
		return nil, badRequest("this deletes ALL location data; call DELETE /locations?confirm=true")
	}

	res, err := c.SQL.ExecContext(c, `DELETE FROM locations`)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()

	return deletedResponse(n), nil
}
