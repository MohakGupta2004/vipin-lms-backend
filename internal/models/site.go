package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type SiteKeyCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type SiteDailyPoint struct {
	Date              string `json:"date"`
	NewVisitors       int    `json:"newVisitors"`
	ReturningVisitors int    `json:"returningVisitors"`
	Pageviews         int    `json:"pageviews"`
	Leads             int    `json:"leads"`
}

type SiteOverview struct {
	Days                int              `json:"days"`
	UniqueVisitors      int              `json:"uniqueVisitors"`
	NewVisitors         int              `json:"newVisitors"`
	ReturningVisitors   int              `json:"returningVisitors"`
	Pageviews           int              `json:"pageviews"`
	AnonymousPageviews  int              `json:"anonymousPageviews"`
	Leads               int              `json:"leads"`
	LeadsByKind         []SiteKeyCount   `json:"leadsByKind"`
	LeadsByCourse       []SiteKeyCount   `json:"leadsByCourse"`
	MasterclassByCourse []SiteKeyCount   `json:"masterclassByCourse"`
	Daily               []SiteDailyPoint `json:"daily"`
	Devices             []SiteKeyCount   `json:"devices"`
	Browsers            []SiteKeyCount   `json:"browsers"`
	Countries           []SiteKeyCount   `json:"countries"`
	Referrers           []SiteKeyCount   `json:"referrers"`
	TopPages            []SiteKeyCount   `json:"topPages"`
}

type SiteLead struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	Phone       string    `json:"phone"`
	CourseSlugs []string  `json:"courseSlugs"`
	CompanyName string    `json:"companyName"`
	TeamSize    string    `json:"teamSize"`
	ExamDate    string    `json:"examDate"`
	Message     string    `json:"message"`
	CASLConsent bool      `json:"caslConsent"`
	SourcePath  string    `json:"sourcePath"`
	VisitorID   *string   `json:"visitorId"`
	CreatedAt   time.Time `json:"createdAt"`
}

type SiteLeadsPage struct {
	Total int        `json:"total"`
	Items []SiteLead `json:"items"`
}

type SiteLeadEmails struct {
	Count  int      `json:"count"`
	Emails []string `json:"emails"`
}

type SiteVisitor struct {
	VisitorID          string    `json:"visitorId"`
	Fingerprint        string    `json:"fingerprint"`
	FingerprintMatches int       `json:"fingerprintMatches"`
	FirstSeenAt        time.Time `json:"firstSeenAt"`
	LastSeenAt         time.Time `json:"lastSeenAt"`
	VisitCount         int       `json:"visitCount"`
	LastSessionID      string    `json:"lastSessionId"`
	Pageviews          int       `json:"pageviews"`
	DeviceType         string    `json:"deviceType"`
	Browser            string    `json:"browser"`
	OS                 string    `json:"os"`
	Language           string    `json:"language"`
	Timezone           string    `json:"timezone"`
	Screen             string    `json:"screen"`
	Country            string    `json:"country"`
	Region             string    `json:"region"`
	City               string    `json:"city"`
	FirstReferrer      string    `json:"firstReferrer"`
	FirstLanding       string    `json:"firstLanding"`
	UTMSource          string    `json:"utmSource"`
	UTMMedium          string    `json:"utmMedium"`
	UTMCampaign        string    `json:"utmCampaign"`
	LeadEmail          *string   `json:"leadEmail"`
	Events             int       `json:"events"`
}

type SiteVisitorsPage struct {
	Total int           `json:"total"`
	Items []SiteVisitor `json:"items"`
}

type SiteEvent struct {
	ID        int64     `json:"id"`
	EventType string    `json:"eventType"`
	Path      string    `json:"path"`
	Label     string    `json:"label"`
	Target    string    `json:"target"`
	SessionID string    `json:"sessionId"`
	CreatedAt time.Time `json:"createdAt"`
}

type SiteRepository struct {
	db *sql.DB
}

func NewSiteRepository(db *sql.DB) *SiteRepository {
	return &SiteRepository{db: db}
}

func (r *SiteRepository) GetOverview(ctx context.Context, days int, now time.Time) (*SiteOverview, error) {
	startOfTodayUTC := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	windowStart := startOfTodayUTC.AddDate(0, 0, -(days - 1))

	overview := &SiteOverview{Days: days}

	baseQuery := `SELECT
		(SELECT COUNT(*) FROM site_visitors WHERE last_seen_at >= $1 AND last_seen_at <= $2),
		(SELECT COUNT(*) FROM site_visitors WHERE first_seen_at >= $1 AND first_seen_at <= $2),
		(SELECT COUNT(*) FROM site_visitors WHERE last_seen_at >= $1 AND last_seen_at <= $2 AND visit_count > 1),
		(SELECT COUNT(*) FROM site_events WHERE created_at >= $1 AND created_at <= $2 AND event_type = 'pageview'),
		(SELECT COUNT(*) FROM site_events WHERE created_at >= $1 AND created_at <= $2 AND event_type = 'pageview' AND visitor_id IS NULL),
		(SELECT COUNT(*) FROM site_leads WHERE created_at >= $1 AND created_at <= $2)`
	err := r.db.QueryRowContext(ctx, baseQuery, windowStart, now).Scan(
		&overview.UniqueVisitors,
		&overview.NewVisitors,
		&overview.ReturningVisitors,
		&overview.Pageviews,
		&overview.AnonymousPageviews,
		&overview.Leads,
	)
	if err != nil {
		return nil, err
	}

	leadsByKind, err := r.keyCountQuery(ctx, `
		SELECT kind, COUNT(*)
		FROM site_leads
		WHERE created_at >= $1 AND created_at <= $2
		GROUP BY kind
		ORDER BY COUNT(*) DESC, kind`, windowStart, now)
	if err != nil {
		return nil, err
	}
	overview.LeadsByKind = leadsByKind

	overview.LeadsByCourse, err = r.keyCountQuery(ctx, `
		SELECT slug, COUNT(*)
		FROM site_leads, unnest(course_slugs) AS slug
		WHERE created_at >= $1 AND created_at <= $2
		GROUP BY slug
		ORDER BY COUNT(*) DESC, slug
		LIMIT 20`, windowStart, now)
	if err != nil {
		return nil, err
	}

	overview.MasterclassByCourse, err = r.keyCountQuery(ctx, `
		SELECT CASE WHEN slug = '' THEN 'none' ELSE slug END AS key, COUNT(*)
		-- a lead with no course still counts, under "none" (unnest of an empty array gives no row)
		FROM site_leads, unnest(CASE WHEN cardinality(course_slugs) = 0 THEN ARRAY[''] ELSE course_slugs END) AS slug
		WHERE created_at >= $1 AND created_at <= $2
		  AND kind = 'masterclass'
		GROUP BY key
		ORDER BY COUNT(*) DESC, key`, windowStart, now)
	if err != nil {
		return nil, err
	}

	overview.Devices, err = r.keyCountQuery(ctx, `
		SELECT device_type, COUNT(*)
		FROM site_visitors
		WHERE last_seen_at >= $1 AND last_seen_at <= $2
		GROUP BY device_type
		ORDER BY COUNT(*) DESC, device_type`, windowStart, now)
	if err != nil {
		return nil, err
	}

	overview.Browsers, err = r.keyCountQuery(ctx, `
		SELECT browser, COUNT(*)
		FROM site_visitors
		WHERE last_seen_at >= $1 AND last_seen_at <= $2
		GROUP BY browser
		ORDER BY COUNT(*) DESC, browser`, windowStart, now)
	if err != nil {
		return nil, err
	}

	overview.Countries, err = r.keyCountQuery(ctx, `
		SELECT country, COUNT(*)
		FROM site_visitors
		WHERE last_seen_at >= $1 AND last_seen_at <= $2
		GROUP BY country
		ORDER BY COUNT(*) DESC, country
		LIMIT 10`, windowStart, now)
	if err != nil {
		return nil, err
	}

	overview.Referrers, err = r.keyCountQuery(ctx, `
		SELECT CASE WHEN first_referrer = '' THEN 'direct' ELSE first_referrer END AS key, COUNT(*)
		FROM site_visitors
		WHERE last_seen_at >= $1 AND last_seen_at <= $2
		GROUP BY key
		ORDER BY COUNT(*) DESC, key
		LIMIT 10`, windowStart, now)
	if err != nil {
		return nil, err
	}

	overview.TopPages, err = r.keyCountQuery(ctx, `
		SELECT path, COUNT(*)
		FROM site_events
		WHERE created_at >= $1 AND created_at <= $2
		  AND event_type = 'pageview'
		GROUP BY path
		ORDER BY COUNT(*) DESC, path
		LIMIT 10`, windowStart, now)
	if err != nil {
		return nil, err
	}

	overview.Daily, err = r.dailyOverview(ctx, windowStart, startOfTodayUTC)
	if err != nil {
		return nil, err
	}

	return overview, nil
}

func (r *SiteRepository) ListLeads(ctx context.Context, kind, course, q string, limit, offset int) (*SiteLeadsPage, error) {
	queryTotal := `SELECT COUNT(*)
		FROM site_leads
		WHERE ($1::text = '' OR kind = $1)
		  AND ($2::text = '' OR $2 = ANY(course_slugs))
		  AND ($3::text = '' OR name ILIKE $4 OR email ILIKE $4 OR company_name ILIKE $4)`
	qLike := "%" + q + "%"
	page := &SiteLeadsPage{}
	err := r.db.QueryRowContext(ctx, queryTotal, kind, course, q, qLike).Scan(&page.Total)
	if err != nil {
		return nil, err
	}

	queryItems := `SELECT id, kind, name, email, phone,
		COALESCE(array_to_json(course_slugs)::text, '[]') AS course_slugs_json,
		company_name, team_size, exam_date, message, casl_consent, source_path, visitor_id, created_at
		FROM site_leads
		WHERE ($1::text = '' OR kind = $1)
		  AND ($2::text = '' OR $2 = ANY(course_slugs))
		  AND ($3::text = '' OR name ILIKE $4 OR email ILIKE $4 OR company_name ILIKE $4)
		ORDER BY created_at DESC, id DESC
		LIMIT $5 OFFSET $6`
	rows, err := r.db.QueryContext(ctx, queryItems, kind, course, q, qLike, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	page.Items = []SiteLead{}
	for rows.Next() {
		var item SiteLead
		var courseSlugsJSON string
		if err := rows.Scan(
			&item.ID,
			&item.Kind,
			&item.Name,
			&item.Email,
			&item.Phone,
			&courseSlugsJSON,
			&item.CompanyName,
			&item.TeamSize,
			&item.ExamDate,
			&item.Message,
			&item.CASLConsent,
			&item.SourcePath,
			&item.VisitorID,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(courseSlugsJSON), &item.CourseSlugs); err != nil {
			return nil, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return page, nil
}

func (r *SiteRepository) ListLeadEmails(ctx context.Context, kind, course string) (*SiteLeadEmails, error) {
	query := `SELECT DISTINCT lower(email)
		FROM site_leads
		WHERE kind = $1
		  AND ($2::text = '' OR $2 = ANY(course_slugs))
		ORDER BY lower(email)`
	rows, err := r.db.QueryContext(ctx, query, kind, course)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := &SiteLeadEmails{Emails: []string{}}
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		result.Emails = append(result.Emails, email)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result.Count = len(result.Emails)
	return result, nil
}

func (r *SiteRepository) ListVisitors(ctx context.Context, returningOnly bool, limit, offset int) (*SiteVisitorsPage, error) {
	queryTotal := `SELECT COUNT(*)
		FROM site_visitors
		WHERE (NOT $1 OR visit_count > 1)`
	page := &SiteVisitorsPage{}
	err := r.db.QueryRowContext(ctx, queryTotal, returningOnly).Scan(&page.Total)
	if err != nil {
		return nil, err
	}

	queryItems := `SELECT visitor_id, fingerprint, first_seen_at, last_seen_at, visit_count,
		last_session_id, pageviews, device_type, browser, os, language, timezone, screen,
		country, region, city, first_referrer, first_landing, utm_source, utm_medium, utm_campaign,
		(SELECT l.email FROM site_leads l WHERE l.visitor_id = v.visitor_id ORDER BY l.created_at DESC, l.id DESC LIMIT 1) AS lead_email,
		(SELECT COUNT(*) FROM site_events e WHERE e.visitor_id = v.visitor_id) AS events,
		CASE WHEN v.fingerprint = '' THEN 0 ELSE (
			SELECT COUNT(*) FROM site_visitors v2 WHERE v2.fingerprint = v.fingerprint AND v2.visitor_id <> v.visitor_id
		) END AS fingerprint_matches
		FROM site_visitors v
		WHERE (NOT $1 OR visit_count > 1)
		ORDER BY last_seen_at DESC, visitor_id
		LIMIT $2 OFFSET $3`
	rows, err := r.db.QueryContext(ctx, queryItems, returningOnly, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	page.Items = []SiteVisitor{}
	for rows.Next() {
		var item SiteVisitor
		var leadEmail sql.NullString
		if err := rows.Scan(
			&item.VisitorID,
			&item.Fingerprint,
			&item.FirstSeenAt,
			&item.LastSeenAt,
			&item.VisitCount,
			&item.LastSessionID,
			&item.Pageviews,
			&item.DeviceType,
			&item.Browser,
			&item.OS,
			&item.Language,
			&item.Timezone,
			&item.Screen,
			&item.Country,
			&item.Region,
			&item.City,
			&item.FirstReferrer,
			&item.FirstLanding,
			&item.UTMSource,
			&item.UTMMedium,
			&item.UTMCampaign,
			&leadEmail,
			&item.Events,
			&item.FingerprintMatches,
		); err != nil {
			return nil, err
		}
		if leadEmail.Valid {
			v := leadEmail.String
			item.LeadEmail = &v
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return page, nil
}

func (r *SiteRepository) VisitorExists(ctx context.Context, visitorID string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM site_visitors WHERE visitor_id = $1)`, visitorID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (r *SiteRepository) ListVisitorEvents(ctx context.Context, visitorID string, limit int) ([]SiteEvent, error) {
	query := `SELECT id, event_type, path, label, target, session_id, created_at
		FROM site_events
		WHERE visitor_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2`
	rows, err := r.db.QueryContext(ctx, query, visitorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []SiteEvent{}
	for rows.Next() {
		var item SiteEvent
		if err := rows.Scan(&item.ID, &item.EventType, &item.Path, &item.Label, &item.Target, &item.SessionID, &item.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, item)
	}
	return events, rows.Err()
}

func (r *SiteRepository) keyCountQuery(ctx context.Context, query string, args ...any) ([]SiteKeyCount, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []SiteKeyCount{}
	for rows.Next() {
		var item SiteKeyCount
		if err := rows.Scan(&item.Key, &item.Count); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *SiteRepository) dailyOverview(ctx context.Context, startDateUTC, endDateUTC time.Time) ([]SiteDailyPoint, error) {
	query := `WITH days AS (
			SELECT generate_series($1::date, $2::date, interval '1 day')::date AS day
		),
		new_visitors AS (
			SELECT (first_seen_at AT TIME ZONE 'UTC')::date AS day, COUNT(*) AS count
			FROM site_visitors
			WHERE (first_seen_at AT TIME ZONE 'UTC')::date BETWEEN $1::date AND $2::date
			GROUP BY day
		),
		returning_visitors AS (
			SELECT (e.created_at AT TIME ZONE 'UTC')::date AS day,
				COUNT(DISTINCT e.visitor_id) FILTER (WHERE (v.first_seen_at AT TIME ZONE 'UTC')::date < (e.created_at AT TIME ZONE 'UTC')::date) AS count
			FROM site_events e
			JOIN site_visitors v ON v.visitor_id = e.visitor_id
			WHERE e.event_type = 'pageview'
			  AND (e.created_at AT TIME ZONE 'UTC')::date BETWEEN $1::date AND $2::date
			GROUP BY day
		),
		pageviews AS (
			SELECT (created_at AT TIME ZONE 'UTC')::date AS day, COUNT(*) AS count
			FROM site_events
			WHERE event_type = 'pageview'
			  AND (created_at AT TIME ZONE 'UTC')::date BETWEEN $1::date AND $2::date
			GROUP BY day
		),
		leads AS (
			SELECT (created_at AT TIME ZONE 'UTC')::date AS day, COUNT(*) AS count
			FROM site_leads
			WHERE (created_at AT TIME ZONE 'UTC')::date BETWEEN $1::date AND $2::date
			GROUP BY day
		)
		SELECT d.day::text,
			COALESCE(n.count, 0),
			COALESCE(r.count, 0),
			COALESCE(p.count, 0),
			COALESCE(l.count, 0)
		FROM days d
		LEFT JOIN new_visitors n ON n.day = d.day
		LEFT JOIN returning_visitors r ON r.day = d.day
		LEFT JOIN pageviews p ON p.day = d.day
		LEFT JOIN leads l ON l.day = d.day
		ORDER BY d.day`

	rows, err := r.db.QueryContext(ctx, query, startDateUTC, endDateUTC)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []SiteDailyPoint{}
	for rows.Next() {
		var item SiteDailyPoint
		if err := rows.Scan(&item.Date, &item.NewVisitors, &item.ReturningVisitors, &item.Pageviews, &item.Leads); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
