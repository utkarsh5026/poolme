# Work-Stealing Strategy - LinkedIn Summary

```
┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
┃     WORK-STEALING SCHEDULER                   ┃
┃     Automatic Load Balancing                  ┃
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛

         📥 Tasks → Global Queue
                    │
      ┌─────────────┼─────────────┐
      ▼             ▼             ▼
  ┌────────┐    ┌────────┐    ┌────────┐
  │Worker 0│    │Worker 1│    │Worker 2│
  │        │    │        │    │        │
  │ [====] │    │ [=   ] │    │ [=====]│
  │   ⬆    │    │   ⬆    │    │   ⬆    │
  └───┼────┘    └───┼────┘    └───┼────┘
      │             │             │
      Own work      │             Busy!
                    │
                    └──── 🏴‍☠️ Steals from
                          Worker 2

🔄 HOW IT WORKS

1. Tasks go to Global Queue
2. Workers grab batches → Local Queue
3. Process own work (LIFO - fast!)
4. Idle? → Steal from busy workers (FIFO)

💡 THE MAGIC

         Worker's Local Queue
    [Front] ← ← ← ← ← ← [Back]
       1   2   3   4   5   6
       ↑               ↑
    Thieves         Owner
    steal here      pops here
    (FIFO)          (LIFO)

    No collision = Zero contention!

⚡ KEY BENEFITS
• Auto load balancing
• Lock-free operations
• Cache-friendly (LIFO)
• Scales with cores

┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
┃ Best for: Unpredictable loads, CPU-bound work ┃
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛
```
