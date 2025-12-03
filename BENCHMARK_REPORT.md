# PoolMe Benchmark Results
Total benchmarks: 5
Strategies tested: 5
Benchmark types: 1

## Performance Summary

### Strategy_CPUBound_AllStrategies

| Strategy | ns/op | Allocs/op | Bytes/op |
|----------|-------|-----------|----------|
| WorkStealing | 5,038,022.00 | 25,138 | 1,593,256 |
| MPMC_Bounded | 5,425,305.00 | 20,109 | 5,382,967 |
| MPMC_Unbounded | 5,573,796.00 | 20,109 | 9,577,209 |
| Channel | 7,985,343.00 | 20,093 | 1,187,881 |
| Bitmask | 8,619,429.00 | 20,093 | 1,187,904 |

**Custom Metrics:**

| Strategy | tasks/sec |
|----------|-----------|
| Channel | 1252294.00 |
| WorkStealing | 1984906.00 |
| MPMC_Bounded | 1843214.00 |
| MPMC_Unbounded | 1794109.00 |
| Bitmask | 1160170.00 |

| Strategy | tasks/sec/worker |
|----------|------------------|
| Channel | 156537.00 |
| WorkStealing | 248113.00 |
| MPMC_Bounded | 230402.00 |
| MPMC_Unbounded | 224264.00 |
| Bitmask | 145021.00 |


## Best Performers

- **Strategy_CPUBound_AllStrategies**: WorkStealing (5,038,022.00 ns/op)
