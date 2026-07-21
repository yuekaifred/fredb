HttpRequest req = HttpRequest.newBuilder()
    .uri(URI.create("https://db.fredyang.com/key/YOUR_KEY"))
    .header("X-Api-Key", "YOUR_API_KEY")
    .DELETE()
    .build();
client.send(req, BodyHandlers.ofString());
