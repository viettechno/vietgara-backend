# Load test summary

Target http://localhost:18000, 50 garages, 50 flow and 25 browse virtual users, steady state 10m.

| Measure | Requests | Avg ms | Median ms | P95 ms | P99 ms | Target |
| --- | --- | --- | --- | --- | --- | --- |
| Reads (NFR-PF-01) | 25175 | 5 | 4 | 10 | 31 | P95 < 300 |
| Writes (NFR-PF-02) | 40550 | 6 | 5 | 12 | 35 | P95 < 800 |
| Events, all (NFR-PF-05) | - | 827 | 1021 | 1044 | 1090 | P95 < 2000 |
|   repair_order.created | - | 675 | 766 | 1032 | 1063 |  |
|   repair_order.closed | - | 979 | 1028 | 1049 | 1117 |  |

Flows completed 3338, failed 0; business transactions 18352 (26.24/s over the whole run); event timeouts 0; HTTP failures 0.00 %.
