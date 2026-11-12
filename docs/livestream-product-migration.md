# Livestream product reference migration

The database reference is `livestream_products.product_id`. `ProductCode` and
the JSON key `product_code` remain aliases in the Go model for compatibility.
All live allocation, order, restock and product-deletion queries use `product_id`.

Older databases can have required `product_id` and `product_code` columns at the
same time. The previous model only wrote `product_code`, causing MySQL 1364
(`Field 'product_id' doesn't have a default value`) when attaching a product.
GORM AutoMigrate does not remove obsolete columns or infer column renames.

Update the application and restart the API normally. `models.MigrateAll` runs
the compatibility migration before AutoMigrate:

- A new table uses only `product_id`.
- A table with only `product_id` already has the expected reference column.
- A table with only `product_code` has that column renamed to `product_id`.
- With both columns, missing `product_id` values are copied from `product_code`.
  The leftover `product_code` column becomes nullable so new inserts can supply
  only `product_id`. Existing legacy values are retained.
- Conflicting nonempty references stop startup with a clear error. They are
  never overwritten automatically.

No table or existing product/live row is deleted. MySQL tests cover preserved
references, snapshots and foreign keys, new inserts after migration, repeated
startup, and conflicting values. Run with a disposable database:

```sh
go test -race ./models ./routes
```

Set `TEST_MYSQL_DSN` through the test environment to enable these database
tests. Without it, connected tests are skipped.

After updating, rerun Studio groups 04 and 05 using **Đã xác thực** and the current
seller/buyer passwords. Register and OTP do not need to run again.
