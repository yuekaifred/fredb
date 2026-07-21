$ch = curl_init("https://db.fredyang.com/range?start=START_KEY&end=END_KEY");
curl_setopt($ch, CURLOPT_HTTPHEADER, ["X-Api-Key: YOUR_API_KEY"]);
curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
$result = curl_exec($ch);
