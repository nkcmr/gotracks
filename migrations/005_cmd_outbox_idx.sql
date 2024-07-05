CREATE TABLE cmd_outbox_consumer_idx (
  user TEXT NOT NULL,
  device TEXT NOT NULL,
  last_outbox_id INTEGER NOT NULL,

  PRIMARY KEY (user, device)
);
