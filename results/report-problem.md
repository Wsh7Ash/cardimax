wrote results/report-problem.md
5**
- series observed: **17**
- verdict: **OVER BUDGET**

## Metrics

| Metric | Type | Series | Labels | Estimate | Projected |
|---|---|---|---|---|---|
| `api_orders_lookup` | counter | 4 | 2 | 8 | 8 |
| `job_runtime_job_id` | gauge | 3 | 2 | 6 | 6 |
| `http_requests_path_raw` | counter | 5 | 1 | 5 | 5 |
| `checkout_debug_events` | counter | 3 | 1 | 3 | 3 |
| `http_requests_total` | counter | 2 | 3 | 2 | 2 |

## Findings

1. **medium** · `job_runtime_job_id{job_id}` — label "job_id" is identity-shaped (3 distinct values observed)
   - fix: allowlist the value set, bucket it, or move it to logs
2. **high** · `http_requests_path_raw{path}` — label "path" has 5 distinct values (> 4)
   - fix: replace with a normalized route template (e.g. /orders/{id}) and keep the raw value in logs
3. **medium** · `checkout_debug_events{debug_message}` — label "debug_message" is identity-shaped (3 distinct values observed)
   - fix: allowlist the value set, bucket it, or move it to logs
4. **medium** · `api_orders_lookup{user_email}` — label "user_email" is identity-shaped (4 distinct values observed)
   - fix: drop the label from the metric; derive per-user breakdowns from logs or exemplars
