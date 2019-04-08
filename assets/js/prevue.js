function mdPreview(text) {
  /*
    var csrf = document.getElementsByName("gorilla.csrf.Token")[0].value;
    var xhr = new XMLHttpRequest();
    xhr.open("POST", '/md_render');

    //Send the proper header information along with the request
    xhr.setRequestHeader("Content-Type", "application/x-www-form-urlencoded");
    xhr.setRequestHeader("X-CSRF-Token", csrf);

    xhr.onreadystatechange = function() { // Call a function when the state changes.
        if (this.readyState === XMLHttpRequest.DONE && this.status === 200) {
            //document.getElementById("preview_content").innerHTML = xhr.responseText;
            return this.responseText;
        }
    }
    xhr.send(encodeURI("md="+text));  
    */

  var url = '/md_render';
  var data = encodeURI("md="+text);
  
  fetch(url, {
    method: 'POST', // or 'PUT'
    body: data, // data can be `string` or {object}!
    headers:{
      'Content-Type': 'application/x-www-form-urlencoded',
      'X-CSRF-Token': document.getElementsByName("gorilla.csrf.Token")[0].value,
    }
  })
  .then(function(response) {
    if(response.ok) {
      return response.text();
    }
    throw new Error('Network response was not ok.');
  })
  .then(function(mdd){
    //console.log(mdd);
    return mdd;
  })
  .catch(error => console.error('Error:', error));  
  //console.log(mdd);
  //return mdd;
}

var editVue = new Vue({
    el: '#tabs',
    data: {
        input: document.getElementById("wikieditor").value,
        previewEnabled: false,
    },
    asyncComputed: {
      compiledMarkdown: {
          /*
        //var editortxt = document.getElementsByName("editor")[0].value;
        var csrf = document.getElementsByName("gorilla.csrf.Token")[0].value;
        var xhr = new XMLHttpRequest();
        xhr.open("POST", '/md_render');

        //Send the proper header information along with the request
        xhr.setRequestHeader("Content-Type", "application/x-www-form-urlencoded");
        xhr.setRequestHeader("X-CSRF-Token", csrf);

        xhr.onreadystatechange = function() { // Call a function when the state changes.
            if (this.readyState === XMLHttpRequest.DONE && this.status === 200) {
                //document.getElementById("preview_content").innerHTML = xhr.responseText;
                return this.responseText;
            }
        }
        xhr.send(encodeURI("md="+this.input));    
        */      
        //return mdPreview(this.input)
        get() {
          var url = '/md_render';
          var data = encodeURI("md="+this.input);
          
          return fetch(url, {
            method: 'POST', // or 'PUT'
            body: data, // data can be `string` or {object}!
            headers:{
              'Content-Type': 'application/x-www-form-urlencoded',
              'X-CSRF-Token': document.getElementsByName("gorilla.csrf.Token")[0].value,
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

/*
  var tabs = [
    {
      name: 'Edit', 
      component: { 
        template: document.getElementById("Edit")
      }
    },
    {
      name: 'Preview',
      component: {
        template: document.getElementById("Preview")
      }
    },
    {
      name: 'Help',
      component: {
        template: document.getElementById("Help")
      }
    }
]

var tabs = new Vue({
  delimiters: ['${', '}'],
  el: '#tabs',
  data: {
    tabs: tabs,
    currentTab: tabs[0]
  }
})
*/
