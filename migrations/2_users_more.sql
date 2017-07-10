-- +migrate Up

ALTER TABLE `users` ADD COLUMN email VARCHAR(255) NOT NULL AFTER id;
ALTER TABLE `users` ADD COLUMN github_login VARCHAR(255) NOT NULL AFTER github_id;

-- +migrate Down
ALTER TABLE `users` DROP COLUMN;
ALTER TABLE `users` DROP COLUMN;
