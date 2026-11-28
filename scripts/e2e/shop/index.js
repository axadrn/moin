// shop answers with its version, the env var GREETING and how many rows
// the table visits has. Every request but /count writes one row first.
const http = require("http");
const { Client } = require("pg");

const version = "v1";

// query opens a connection for each query, so a restore that swaps the
// database under the app never leaves it with a dead one.
async function query(sql, params) {
  const client = new Client();
  await client.connect();
  try {
    return await client.query(sql, params);
  } finally {
    await client.end();
  }
}

http
  .createServer(async (req, res) => {
    try {
      await query("CREATE TABLE IF NOT EXISTS visits (at timestamptz DEFAULT now(), path text)");
      if (req.url !== "/count") {
        await query("INSERT INTO visits (path) VALUES ($1)", [req.url]);
      }
      const { rows } = await query("SELECT count(*) AS n FROM visits");
      console.log("visit", req.url, "rows", rows[0].n);
      res.end(`shop ${version} greeting=${process.env.GREETING || ""} rows=${rows[0].n}\n`);
    } catch (err) {
      console.error("no database:", err.message);
      res.statusCode = 503;
      res.end(`no database: ${err.message}\n`);
    }
  })
  .listen(3000, () => console.log("shop listens on 3000"));
