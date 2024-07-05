CREATE UNIQUE INDEX idx_users_user ON users(user);
INSERT INTO users (user) VALUES ('nkcmr');

CREATE TABLE location_reports_v2 (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    device TEXT NOT NULL,
    data JSON NOT NULL
);

INSERT INTO location_reports_v2
SELECT id, 1 AS user_id, 'b084c7f9-56b4-490c-8da3-6d3cb768ea78' AS device, data
FROM location_reports;

UPDATE sqlite_sequence
SET seq = (
  SELECT seq FROM sqlite_sequence WHERE name = 'location_reports'
)
WHERE name = 'location_reports_v2';
CREATE INDEX idx_loc_report_user_device ON location_reports_v2(user_id, device);
CREATE INDEX idx_loc_report_user ON location_reports_v2(user_id);
