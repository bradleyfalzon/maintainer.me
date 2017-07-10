-- +migrate Up
CREATE TABLE sessions (
      token CHAR(43) PRIMARY KEY,
      data BLOB NOT NULL,
      expiry TIMESTAMP(6) NOT NULL
);
CREATE INDEX sessions_expiry_idx ON sessions (expiry);

-- +migrate Down
DROP TABLE `sessions`;
