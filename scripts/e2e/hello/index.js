// hello has no Dockerfile: the server builds it with Railpack.
require("http")
  .createServer((req, res) => res.end("hello from a sudo user\n"))
  .listen(process.env.PORT || 3000);
