// Tabs based on https://codepen.io/samrose3/pen/wgaYEz
Vue.component('tabs', {
  template: `
    <div>
      <ul class="tabs sub" id="subtabs">
        <li v-for="tab in tabs" :class="[{ 'is-active': tab.isActive }, 'tab-title']">
          <a class="tablinks" @click="selectTab(tab)">{{ tab.name }}</a>
        </li>
      </ul>

      <div class="tabs-details">
        <slot></slot>
      </div>
    </div>
  `,
  data() {
    return { tabs: [] }
  },
  created() {
    this.tabs = this.$children;
  },
  methods: {
    selectTab: function(selectedTab) {
      this.tabs.forEach(tab => {
        tab.isActive = (tab.name == selectedTab.name);
      });
    }
  }
});

Vue.component('tab', {
  props: {
    name: { required: true },
    selected: { default: false }
  },
  template: `<div v-show="isActive"><slot></slot></div>`,
  data() {
    return { isActive: false }
  },
  mounted() {
    this.isActive = this.selected;
  }
});

/* 
var tabsVue = new Vue({
  el: '#tabs',
});
*/
