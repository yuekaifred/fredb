$ch = curl_init("https://db.fredyang.com/key/YOUR_KEY");
curl_setopt($ch, CURLOPT_CUSTOMREQUEST, "PUT");
curl_setopt($ch, CURLOPT_POSTFIELDS, "YOUR_VALUE");
curl_setopt($ch, CURLOPT_HTTPHEADER, ["X-Api-Key: YOUR_API_KEY"]);
$result = curl_exec($ch);
