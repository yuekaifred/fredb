await fetch("https://db.fredyang.com/key/hello", {
  method: "PUT",
  headers: { "X-Api-Key": API_KEY },
  body: "world",
});
