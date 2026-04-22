-- name: GetProductByID :one
SELECT
  sqlc.embed(products),
  sqlc.embed(currencies)
FROM
  products
  JOIN currencies ON products.currency_id = currencies.id
WHERE
  product_id = sqlc.arg (product_id);

-- name: GetProductBySKU :one
SELECT
  sqlc.embed(products),
  sqlc.embed(currencies)
FROM
  products
  JOIN currencies ON products.currency_id = currencies.id
WHERE
  sku = sqlc.arg (sku);

-- name: GetCurrencyByCode :one
SELECT
  id
FROM
  currencies
WHERE
  code = sqlc.arg (code);

-- name: ListProducts :many
SELECT
  sqlc.embed(products),
  sqlc.embed(currencies)
FROM
  products
  JOIN currencies ON products.currency_id = currencies.id
ORDER BY
  created_at DESC
LIMIT
  $1
OFFSET
  $2;

-- name: CreateProduct :one
WITH
  created AS (
    INSERT INTO
      products (
        sku,
        name,
        description,
        price,
        currency_id,
        category_id,
        status
      )
    VALUES
      (
        sqlc.arg (sku),
        sqlc.arg (name),
        sqlc.narg (description),
        sqlc.arg (price),
        sqlc.arg (currency_id),
        sqlc.narg (category_id),
        sqlc.arg (status)
      )
    RETURNING
      *
  )
SELECT
  sqlc.embed(products),
  sqlc.embed(currencies)
FROM
  created
  JOIN products ON products.product_id = created.product_id
  LEFT JOIN currencies ON products.currency_id = currencies.id;

-- name: UpdateProduct :one
WITH
  updated AS (
    UPDATE products
    SET
      sku = sqlc.arg (sku),
      name = sqlc.arg (name),
      description = sqlc.narg (description),
      price = sqlc.arg (price),
      currency_id = sqlc.arg (currency_id),
      category_id = sqlc.narg (category_id),
      status = sqlc.arg (status),
      updated_at = NOW()
    WHERE
      products.product_id = sqlc.arg (product_id)
    RETURNING
      *
  )
SELECT
  sqlc.embed(products),
  sqlc.embed(currencies)
FROM
  updated
  JOIN products ON products.product_id = updated.product_id
  LEFT JOIN currencies ON products.currency_id = currencies.id;

-- name: DeleteProduct :one
DELETE FROM products
WHERE
  product_id = sqlc.arg (product_id)
RETURNING
  *;
