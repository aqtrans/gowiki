function openTab(event, tabName) {
    console.log(event.type);

    // Declare all variables
    var i, tabcontent, tablinks;

    // Get all elements with class="tabcontent" and hide them
    tabcontent = document.getElementsByClassName("tabcontent");
    for (i = 0; i < tabcontent.length; i++) {
        tabcontent[i].style.display = "none";
    }

    // Get all elements with class="tablinks" and remove the class "active"
    tablinks = document.getElementsByClassName("tablinks");
    for (i = 0; i < tablinks.length; i++) {
        tablinks[i].className = tablinks[i].className.replace(" is-active", "");
    }

    // Show the current tab, and add an "active" class to the button that opened the tab
    document.getElementById(tabName).style.display = "block";
    event.currentTarget.className += " is-active";

    if (tabName == "Preview") {
        var editortxt = document.getElementsByName("editor")[0].value;
        var csrf = document.getElementsByName("gorilla.csrf.Token")[0].value;
        var params = "md="+editortxt;
        var xhr = new XMLHttpRequest();
        xhr.open("POST", '/md_render', true);

        //Send the proper header information along with the request
        xhr.setRequestHeader("Content-Type", "application/x-www-form-urlencoded");
        xhr.setRequestHeader("X-CSRF-Token", csrf);

        xhr.onreadystatechange = function() { // Call a function when the state changes.
            if (this.readyState === XMLHttpRequest.DONE && this.status === 200) {
                document.getElementById("preview_content").innerHTML = xhr.responseText;
            }
        }
        xhr.send(params);
        /*
        $.ajax({
            type: "POST",
            url: "/md_render",
            data: { md: editortxt, 'gorilla.csrf.Token': csrf },
            success: function (msg) {
                document.getElementById("preview_content").innerHTML = msg;
            },
            dataType: "html"
        });
        */
    }
}  

function doIt(event) {
    var defaultTab = document.getElementById('subtabs').getElementsByClassName('is-active')[0];
    if (defaultTab){
        defaultTab.click();
    }

    var edit = document.getElementById('edit-tab');
    if (edit){
        edit.addEventListener('click', function(event){ 
            openTab(event, "Edit"), false
        });
    }
    var preview = document.getElementById('preview-tab');
    if (preview){
        preview.addEventListener('click', function(event){
            openTab(event, "Preview"), false
        });
    } 
    var help = document.getElementById('help-tab');
    if (help){
        help.addEventListener('click', function(event){
            openTab(event, "Help"), false
        });
    }
    var contentTab = document.getElementById('content-tab');
    if (contentTab){
        contentTab.addEventListener('click', function(event){
            openTab(event, "Content"), false
        });
    }
    var diffTab = document.getElementById('diff-tab');
    if (diffTab){
        diffTab.addEventListener('click', function(event){
            openTab(event, "Diff"), false
        });
    }                   
}

document.addEventListener('DOMContentLoaded', doIt, false);


