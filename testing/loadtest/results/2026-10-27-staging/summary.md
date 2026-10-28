# Load test summary

Target https://vietgara-api-staging.viettechno.com, 50 garages, 50 flow and 25 browse virtual users, steady state 10m.

| Measure | Requests | Avg ms | Median ms | P95 ms | P99 ms | Target |
| --- | --- | --- | --- | --- | --- | --- |
| Reads (NFR-PF-01) | 20033 | 192 | 187 | 194 | 369 | P95 < 300 |
| Writes (NFR-PF-02) | 30258 | 191 | 189 | 196 | 200 | P95 < 800 |
| Events, all (NFR-PF-05) | - | 760 | 637 | 1081 | 1085 | P95 < 2000 |
|   repair_order.created | - | 740 | 634 | 1079 | 1084 |  |
|   repair_order.closed | - | 780 | 1054 | 1082 | 1086 |  |

Flows completed 2512, failed 0; business transactions 13802 (19.77/s over the whole run); event timeouts 0; HTTP failures 0.00 %.
