await fetch("https://db.fredyang.com/key/hello", {
  method: "DELETE",
  headers: { "X-Api-Key": API_KEY },
});
