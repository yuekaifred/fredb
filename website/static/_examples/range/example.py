requests.get(
  "https://db.fredyang.com/range",
  params={"start": "START_KEY", "end": "END_KEY"},
  headers={"X-Api-Key": "YOUR_API_KEY"},
)
