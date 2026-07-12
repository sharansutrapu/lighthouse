const fs = require('fs');
fetch('http://localhost:8000/api/containers', { headers: { 'Authorization': 'Bearer ' + 'skip' }})
  .then(res => res.json())
  .then(data => console.log(Object.keys(data[0] || {})))
  .catch(console.error);
