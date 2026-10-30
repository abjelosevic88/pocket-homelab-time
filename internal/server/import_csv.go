package server

import (
	"bytes"
	"crypto/sha1"
	"encoding/csv"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-time/internal/store"
)

// ---- CSV import: Toggl Track, Clockify, Kimai, Harvest or Pocket Time's own CSV ----
//
// The columns are found by name, so any of those exports works as is.
// Projects are matched by name and created when missing. Each row gets an
// external id from its content, so importing the same file twice adds nothing.

var csvCols = map[string][]string{
	"title":      {"description", "title", "task", "activity", "notes", "note"},
	"project":    {"project", "project name"},
	"client":     {"client", "customer"},
	"tags":       {"tags", "tag"},
	"billable":   {"billable"},
	"start_date": {"start date", "date", "begin date", "spent date"},
	"start_time": {"start time", "start", "from", "begin", "begin time"},
	"end_date":   {"end date"},
	"end_time":   {"end time", "end", "to"},
	"duration":   {"duration", "duration (h)", "duration (decimal)", "hours", "minutes", "duration (h:mm)"},
	"note":       {"note", "notes", "comment"},
}

// csvTime reads a date and a time in the many ways exports write them.
func csvTime(date, clock string) (time.Time, bool) {
	date, clock = strings.TrimSpace(date), strings.TrimSpace(clock)
	if t, ok := store.ParseTime(clock); ok && strings.Contains(clock, "-") {
		return t, true
	}
	if t, ok := store.ParseTime(date); ok && strings.Contains(date, ":") {
		return t, true
	}
	dates := []string{"2006-01-02", "02/01/2006", "01/02/2006", "02.01.2006", "2.1.2006", "1/2/2006", "2006/01/02"}
	clocks := []string{"15:04:05", "15:04", "3:04 PM", "3:04:05 PM", "03:04 PM", "3:04PM"}
	if clock == "" {
		clock = "09:00"
	}
	for _, d := range dates {
		for _, c := range clocks {
			if t, err := time.ParseInLocation(d+" "+c, date+" "+clock, time.Local); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// csvDuration reads "1:30:00", "1:30", "1.5" (hours) or "90" (with a minutes header).
func csvDuration(v, header string) (int, bool) {
	v = strings.TrimSpace(strings.ReplaceAll(v, ",", "."))
	if v == "" {
		return 0, false
	}
	if strings.Contains(v, ":") {
		parts := strings.Split(v, ":")
		total := 0
		for _, p := range parts {
			n, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil {
				return 0, false
			}
			total = total*60 + n
		}
		if len(parts) == 2 {
			total *= 60
		}
		return total, true
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, false
	}
	if header == "minutes" {
		return int(f * 60), true
	}
	return int(f * 3600), true
}

func (s *Server) handleImportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	b, err := s.readBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "could not read the file")
		return
	}
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	sep := ','
	if first, _, _ := bytes.Cut(b, []byte("\n")); bytes.Count(first, []byte(";")) > bytes.Count(first, []byte(",")) {
		sep = ';'
	}
	cr := csv.NewReader(bytes.NewReader(b))
	cr.Comma = sep
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	head, err := cr.Read()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "this file has no header row")
		return
	}
	idx := map[string]int{}
	hname := map[string]string{}
	for field, names := range csvCols {
		for _, n := range names {
			for i, h := range head {
				if strings.EqualFold(strings.TrimSpace(h), n) {
					if _, ok := idx[field]; !ok {
						idx[field], hname[field] = i, strings.ToLower(strings.TrimSpace(h))
					}
				}
			}
		}
	}
	if _, ok := idx["start_date"]; !ok {
		writeErr(w, http.StatusBadRequest, "no date column found (expected Start date / Date)")
		return
	}
	get := func(row []string, field string) string {
		i, ok := idx[field]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	st, _ := s.store.GetSettings(ctx, uid)
	projects := map[string]*int64{}
	counts := map[string]int{"entries": 0, "projects": 0, "skipped": 0}
	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			counts["skipped"]++
			continue
		}
		start, ok := csvTime(get(row, "start_date"), get(row, "start_time"))
		if !ok {
			counts["skipped"]++
			continue
		}
		var end time.Time
		if et := get(row, "end_time"); et != "" {
			if t, ok := csvTime(firstNonEmpty(get(row, "end_date"), get(row, "start_date")), et); ok {
				end = t
				if end.Before(start) {
					end = end.AddDate(0, 0, 1) // past midnight
				}
			}
		}
		if end.IsZero() {
			d, ok := csvDuration(get(row, "duration"), hname["duration"])
			if !ok {
				counts["skipped"]++
				continue
			}
			end = start.Add(time.Duration(d) * time.Second)
		}
		e := store.Entry{Source: "import", Title: clip(get(row, "title"), 300), Tags: store.JoinTags(get(row, "tags"))}
		if n := get(row, "note"); n != "" && idx["note"] != idx["title"] {
			e.Note = clip(n, 4000)
		}
		if pn := get(row, "project"); pn != "" {
			key := strings.ToLower(pn)
			pid, seen := projects[key]
			if !seen {
				if p, err := s.store.ProjectByName(ctx, uid, pn); err == nil {
					pid = &p.ID
				} else {
					p := store.Project{Name: clip(pn, 120), Client: clip(get(row, "client"), 120)}
					if s.store.CreateProject(ctx, uid, &p) == nil {
						pid = &p.ID
						counts["projects"]++
					}
				}
				projects[key] = pid
			}
			e.ProjectID = pid
		}
		switch strings.ToLower(get(row, "billable")) {
		case "yes", "true", "1", "y":
			e.Billable = true
		case "no", "false", "0", "n":
		default:
			e.Billable = s.projectBillable(ctx, uid, e.ProjectID, st)
		}
		in := entryInput{StartedAt: store.Stamp(start), EndedAt: store.Stamp(end)}
		if m := finishTimes(&in, nil, &e); m != "" {
			counts["skipped"]++
			continue
		}
		h := sha1.Sum([]byte(strings.Join(row, "\x1f")))
		e.ExternalID = "csv:" + hex.EncodeToString(h[:10])
		if err := s.store.CreateEntry(ctx, uid, &e); err != nil {
			counts["skipped"]++
			continue
		}
		counts["entries"]++
	}
	writeJSON(w, http.StatusOK, counts)
}
