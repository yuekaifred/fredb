HttpRequest req = HttpRequest.newBuilder()
    .uri(URI.create("https://db.fredyang.com/range?start=START_KEY&end=END_KEY"))
    .header("X-Api-Key", "YOUR_API_KEY")
    .GET()
    .build();
client.send(req, BodyHandlers.ofString());
