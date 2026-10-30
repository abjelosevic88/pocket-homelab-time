package store

import "context"

const projectCols = `id, user_id, name, color, client, billable, rate, goal_min, archived, sort, created_at, updated_at`

func scanProject(row interface{ Scan(...any) error }) (*Project, error) {
	var p Project
	var bill, arch int
	if err := row.Scan(&p.ID, &p.UserID, &p.Name, &p.Color, &p.Client, &bill, &p.Rate, &p.GoalMin, &arch, &p.Sort, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	p.Billable, p.Archived = bill == 1, arch == 1
	return &p, nil
}

// ListProjects returns the person's projects (archived ones too when asked).
func (s *Store) ListProjects(ctx context.Context, uid int64, archived bool) ([]Project, error) {
	q := `SELECT ` + projectCols + ` FROM projects WHERE user_id = ?`
	if !archived {
		q += ` AND archived = 0`
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY archived, sort, name COLLATE NOCASE`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// GetProject returns one of the person's projects.
func (s *Store) GetProject(ctx context.Context, uid, id int64) (*Project, error) {
	p, err := scanProject(s.DB.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE user_id = ? AND id = ?`, uid, id))
	return p, notFound(err)
}

// CreateProject adds a project.
func (s *Store) CreateProject(ctx context.Context, uid int64, p *Project) error {
	now := Now()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO projects (user_id, name, color, client, billable, rate, goal_min, archived, sort, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uid, p.Name, p.Color, p.Client, boolInt(p.Billable), p.Rate, p.GoalMin, boolInt(p.Archived), p.Sort, now, now)
	if err != nil {
		return conflict(err)
	}
	p.ID, _ = res.LastInsertId()
	p.UserID, p.CreatedAt, p.UpdatedAt = uid, now, now
	return nil
}

// UpdateProject saves a project.
func (s *Store) UpdateProject(ctx context.Context, uid int64, p *Project) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE projects SET name = ?, color = ?, client = ?, billable = ?, rate = ?, goal_min = ?, archived = ?, sort = ?, updated_at = ? WHERE user_id = ? AND id = ?`,
		p.Name, p.Color, p.Client, boolInt(p.Billable), p.Rate, p.GoalMin, boolInt(p.Archived), p.Sort, Now(), uid, p.ID)
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteProject removes a project; its entries stay, without a project.
func (s *Store) DeleteProject(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM projects WHERE user_id = ? AND id = ?`, uid, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ProjectByName finds a project by name (case-insensitive).
func (s *Store) ProjectByName(ctx context.Context, uid int64, name string) (*Project, error) {
	p, err := scanProject(s.DB.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE user_id = ? AND name = ? COLLATE NOCASE`, uid, name))
	return p, notFound(err)
}
