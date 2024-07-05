CREATE TABLE cmd_outbox (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user TEXT NOT NULL,
  device TEXT NOT NULL,
  data JSON NOT NULL,
  when_created INTEGER NOT NULL,
  when_expires INTEGER
);
CREATE INDEX idx_cmd_outbox_user_device ON cmd_outbox(user, device);

DROP TABLE location_reports;
ALTER TABLE location_reports_v2 RENAME TO location_reports;
