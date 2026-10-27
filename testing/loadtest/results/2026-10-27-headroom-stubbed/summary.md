# Load test summary

Target http://localhost:18000, 50 garages, 150 flow and 75 browse virtual users, steady state 5m.

| Measure | Requests | Avg ms | Median ms | P95 ms | P99 ms | Target |
| --- | --- | --- | --- | --- | --- | --- |
| Reads (NFR-PF-01) | 39522 | 5 | 4 | 9 | 19 | P95 < 300 |
| Writes (NFR-PF-02) | 58065 | 6 | 5 | 12 | 22 | P95 < 800 |
| Events, all (NFR-PF-05) | - | 870 | 1021 | 1048 | 1284 | P95 < 2000 |
|   repair_order.created | - | 773 | 769 | 1048 | 1292 |  |
|   repair_order.closed | - | 967 | 1027 | 1048 | 1105 |  |

Flows completed 5324, failed 0; business transactions 27697 (68.99/s over the whole run); event timeouts 0; HTTP failures 0.00 %.
