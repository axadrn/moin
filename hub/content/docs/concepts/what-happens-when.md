---
title: "What happens when"
description: "A server restarts, a bucket is gone, a copy is broken: what moin does, tried on real servers"
group: "Servers and data"
order: 7
---

Things break. We broke them on purpose, on real servers, and wrote down what moin did.

| When | Then |
| --- | --- |
| A new version fails to start | The version before starts again. The deploy says why. |
| The server restarts during a deploy | The version before runs again when the server is back. You can deploy at once. |
| The daemon dies during a deploy | The same, seconds later. |
| The daemon dies during a copy | Your database never noticed. The next copy runs on time. |
| The daemon dies during a restore | Your database is whole. The app starts, and the restore says which data it runs on. |
| Your bucket is gone | Your app keeps running. Backups show red. The next copy comes when the bucket is back. |
| A copy is broken | The daily check finds it. Backups show red and you get an [alert](/docs/guides/alerts). |

**One thing moin cannot know.** If a new version changed your database before it failed, the version before runs on the changed database. Mostly that works. If it does not, deploy again, or go back to the copy moin made before that deploy.

More: [How backups work](/docs/concepts/backups).
