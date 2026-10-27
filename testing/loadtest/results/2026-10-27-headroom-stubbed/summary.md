# Load test summary

Target http://localhost:18000, 50 garages, 150 flow and 75 browse virtual users, steady state 5m.

| Measure | Requests | Avg ms | Median ms | P95 ms | P99 ms | Target |
| --- | --- | --- | --- | --- | --- | --- |
| Reads (NFR-PF-01) | 40357 | 5 | 4 | 11 | 27 | P95 < 300 |
| Writes (NFR-PF-02) | 64647 | 7 | 5 | 15 | 29 | P95 < 800 |
| Events, all (NFR-PF-05) | - | 872 | 1021 | 1061 | 1296 | P95 < 2000 |
|   repair_order.created | - | 783 | 771 | 1270 | 1526 |  |
|   repair_order.closed | - | 960 | 1027 | 1055 | 1122 |  |

Flows completed 5332, failed 0; business transactions 29363 (73.51/s over the whole run); event timeouts 0; HTTP failures 0.00 %.
