fetch("https://db.fredyang.com/key/YOUR_KEY", {
  headers: { "X-Api-Key": "YOUR_API_KEY" }
}).then(r => r.text())
