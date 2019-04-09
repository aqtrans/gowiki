var editVue = new Vue({
    el: '#tabs',
    data: {
        input: document.getElementById("wikieditor").value,
        previewEnabled: false,
    },
    asyncComputed: {
      compiledMarkdown: {
        get() {
          var url = '/md_render';
          var data = encodeURI("md="+this.input);
          
          return fetch(url, {
            method: 'POST', // or 'PUT'
            body: data, // data can be `string` or {object}!
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
            return md;
          })
          .catch(error => console.error('Error:', error));
        },
        default: ""
      
      }
    },
    methods: {
      update: _.debounce(function (e) {
        this.input = e.target.value
      }, 300)
    }
});
