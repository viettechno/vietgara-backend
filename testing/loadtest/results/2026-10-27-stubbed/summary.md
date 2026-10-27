# Load test summary

Target http://localhost:18000, 50 garages, 50 flow and 25 browse virtual users, steady state 10m.

| Measure | Requests | Avg ms | Median ms | P95 ms | P99 ms | Target |
| --- | --- | --- | --- | --- | --- | --- |
| Reads (NFR-PF-01) | 24767 | 4 | 4 | 8 | 16 | P95 < 300 |
| Writes (NFR-PF-02) | 36476 | 5 | 5 | 8 | 15 | P95 < 800 |
| Events, all (NFR-PF-05) | - | 830 | 1023 | 1042 | 1064 | P95 < 2000 |
|   repair_order.created | - | 678 | 766 | 1032 | 1049 |  |
|   repair_order.closed | - | 982 | 1029 | 1048 | 1070 |  |

Flows completed 3346, failed 0; business transactions 17391 (24.85/s over the whole run); event timeouts 0; HTTP failures 0.00 %.
