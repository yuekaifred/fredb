await fetch("https://db.fredyang.com/range?start=a&end=z", {
  headers: { "X-Api-Key": API_KEY },
}).then(r => r.json());
