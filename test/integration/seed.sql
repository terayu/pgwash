-- pgwash 結合検証用シードデータ
-- cutoff = 2026-08-01T00:00:00+00:00 を前提とする（run.sh の CUTOFF と一致させること）
--
-- テーブル構成（検証依頼の対応表）:
--   trend_a     : ptime あり。--tables 指定 → Trim。境界(=cutoff)行を含む
--   trend_b     : ptime あり。非選択        → CopyAll（ptimeありなので差分キャッチアップ対象）
--   master_a    : ptime なし。非選択        → CopyAll（差分は TRUNCATE+全件再コピー）
--   old_kiki    : ptime あり。--drop-tables 指定 → Drop
--   trend_null  : ptime あり、全行NULL。--tables 指定 → Trim（0件になる想定）
--   trend_empty : ptime あり。--tables 指定。フェーズA時点でcutoff以降が0件 → MAX(ptime) NULL分岐の検証

DROP DATABASE IF EXISTS testdb;
DROP ROLE IF EXISTS appuser;

CREATE ROLE appuser LOGIN PASSWORD 'appuser';
CREATE DATABASE testdb OWNER postgres;

\c testdb

CREATE TABLE master_a (
    id   integer PRIMARY KEY,
    name text
);
INSERT INTO master_a (id, name) VALUES
    (1, 'kiki-A'), (2, 'kiki-B'), (3, 'kiki-C'), (4, 'kiki-D'), (5, 'kiki-E');

CREATE TABLE trend_a (
    id    integer PRIMARY KEY,
    ptime timestamp without time zone,
    val   double precision
);
INSERT INTO trend_a (id, ptime, val) VALUES
    (1, '2026-07-20 00:00:00', 1.1),
    (2, '2026-07-25 00:00:00', 1.2),
    (3, '2026-07-31 23:00:00', 1.3),
    (4, '2026-08-01 00:00:00', 2.1),  -- 境界(=cutoff)
    (5, '2026-08-01 00:00:00', 2.2),  -- 境界(=cutoff) もう1行（同一ptime複数行の重複防止確認用）
    (6, '2026-08-02 00:00:00', 2.3),
    (7, '2026-08-03 00:00:00', 2.4);  -- フェーズA時点でのMAX(ptime)になる想定

CREATE TABLE trend_b (
    id    integer PRIMARY KEY,
    ptime timestamp without time zone,
    val   double precision
);
INSERT INTO trend_b (id, ptime, val) VALUES
    (1, '2026-07-10 00:00:00', 9.1),
    (2, '2026-07-11 00:00:00', 9.2),
    (3, '2026-08-05 00:00:00', 9.3),
    (4, '2026-08-05 12:00:00', 9.4),
    (5, '2026-08-06 00:00:00', 9.5); -- フェーズA時点でのMAX(ptime)になる想定

CREATE TABLE old_kiki (
    id    integer PRIMARY KEY,
    ptime timestamp without time zone,
    val   double precision
);
INSERT INTO old_kiki (id, ptime, val) VALUES
    (1, '2020-01-01 00:00:00', 0.1),
    (2, '2020-01-02 00:00:00', 0.2),
    (3, '2020-01-03 00:00:00', 0.3);

CREATE TABLE trend_null (
    id    integer PRIMARY KEY,
    ptime timestamp without time zone,
    val   double precision
);
INSERT INTO trend_null (id, ptime, val) VALUES
    (1, NULL, 5.1), (2, NULL, 5.2), (3, NULL, 5.3), (4, NULL, 5.4);

CREATE TABLE trend_empty (
    id    integer PRIMARY KEY,
    ptime timestamp without time zone,
    val   double precision
);
INSERT INTO trend_empty (id, ptime, val) VALUES
    (1, '2026-07-10 00:00:00', 7.1),
    (2, '2026-07-15 00:00:00', 7.2),
    (3, '2026-07-20 00:00:00', 7.3); -- 全行cutoffより前 → フェーズAで0件、MAX(ptime)がNULLになる

-- テーブルレベルGRANT（pg_dump --schema-only に含まれるかの検証用。設計案 7.2節）
GRANT SELECT ON trend_a TO appuser;

-- DBレベルACL（設計案 7.1節・検証項目D）。デフォルトから変更し、新DBへの引き継ぎを検証する。
REVOKE CONNECT ON DATABASE testdb FROM PUBLIC;
GRANT CONNECT ON DATABASE testdb TO appuser;
