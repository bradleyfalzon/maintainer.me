-- +migrate Up
ALTER TABLE `users` ADD COLUMN filter_default_discard TINYINT NOT NULL DEFAULT 1 AFTER github_token;

-- +migrate Down
ALTER TABLE `users` DROP COLUMN filter_default_discard;
