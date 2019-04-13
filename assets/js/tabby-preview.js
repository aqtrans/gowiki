var tabs = new Tabby('[data-tabs]');

function mdPreview() {
    fetch('/md_render', {
        method: 'POST', // or 'PUT'
        body: JSON.stringify({md: document.querySelector("#wikieditor").value}), // data can be `string` or {object}!
        headers:{
            'Content-Type': 'application/x-www-form-urlencoded',
            'X-CSRF-Token': document.querySelector("input[name='gorilla.csrf.Token']").value,
        }
    })
    .then(function(response) {
        if(response.ok) {
            return response.text();
        }
        throw new Error('Network response was not ok.');
    })
    .then((md) => {
        document.querySelector('#previewcontent').innerHTML = md;
    })
    .catch(error => console.error('Error:', error));
}

document.addEventListener('tabby', function (event) {
    var tab = event.target;
    if (tab.id === 'preview-tab') {
        mdPreview();
    }
	var content = event.detail.content;
}, false);