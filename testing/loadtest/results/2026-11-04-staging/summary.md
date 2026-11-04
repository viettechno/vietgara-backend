# Load test summary

Target https://vietgara-api-staging.viettechno.com, 50 garages, 50 flow and 25 browse virtual users, steady state 10m.

| Measure | Requests | Avg ms | Median ms | P95 ms | P99 ms | Target |
| --- | --- | --- | --- | --- | --- | --- |
| Reads (NFR-PF-01) | 19125 | 219 | 215 | 222 | 411 | P95 < 300 |
| Writes (NFR-PF-02) | 28652 | 218 | 217 | 224 | 228 | P95 < 800 |
| Events, all (NFR-PF-05) | - | 857 | 692 | 1163 | 1170 | P95 < 2000 |
|   repair_order.created | - | 847 | 690 | 1162 | 1166 |  |
|   repair_order.closed | - | 868 | 694 | 1164 | 1171 |  |

Flows completed 2378, failed 1; business transactions 13091 (18.79/s over the whole run); event timeouts 0; HTTP failures 0.00 %.
